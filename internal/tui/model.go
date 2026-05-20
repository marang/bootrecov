package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Model contains application state

type Model struct {
	Backups       []BootBackup
	Entries       []GrubEntry
	status        string
	cursor        int
	mode          mode
	confirmDelete bool
	deleteTarget  string
	busy          bool
	task          taskKind
	taskLabel     string
	taskDetail    string
	taskOutput    <-chan string
	taskFrame     int
	width         int
	height        int
	backupList    list.Model
	entryTable    table.Model
	activityWidth int
	help          help.Model
	keys          keyMap
}

type mode int

const (
	modeBackups mode = iota
	modeEntries
)

type taskKind int

const (
	taskNone taskKind = iota
	taskBackup
	taskActivate
	taskDeactivate
	taskReconcile
	taskDelete
	taskInstallHook
	taskUninstallHook
	taskRemoveEntry
)

type taskDoneMsg struct {
	task          taskKind
	status        string
	errStatus     string
	backups       []BootBackup
	entries       []GrubEntry
	cursorName    string
	cursorEntryID string
	mode          mode
}

type activityTickMsg struct{}

type taskOutputMsg struct {
	line string
	ok   bool
}

type taskCmdFactory func(chan<- string) tea.Cmd

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	Backup    key.Binding
	Toggle    key.Binding
	Reconcile key.Binding
	Recovery  key.Binding
	Hook      key.Binding
	Delete    key.Binding
	Remove    key.Binding
	Switch    key.Binding
	Help      key.Binding
	Quit      key.Binding
	Confirm   key.Binding
	Cancel    key.Binding
}

type helpKeyMap struct {
	short []key.Binding
	full  [][]key.Binding
}

func (k helpKeyMap) ShortHelp() []key.Binding {
	return k.short
}

func (k helpKeyMap) FullHelp() [][]key.Binding {
	return k.full
}

type backupItem struct {
	backup BootBackup
}

func (i backupItem) FilterValue() string {
	return strings.TrimSpace(i.backup.Name + " " + i.backup.KernelVersion + " " + statusString(i.backup))
}

type backupDelegate struct{}

func (d backupDelegate) Height() int {
	return 2
}

func (d backupDelegate) Spacing() int {
	return 1
}

func (d backupDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd {
	return nil
}

func (d backupDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	backupItem, ok := item.(backupItem)
	if !ok {
		return
	}
	b := backupItem.backup
	line := fmt.Sprintf("%s %s", b.Name, statusBadge(b))
	if b.GrubEntryExists {
		line += " " + tagStyle.Render("[grub]")
	}
	meta := mutedStyle.Render("  " + backupMetaSummary(b))
	if index == m.Index() {
		_, _ = fmt.Fprint(w, activeStyle.Render("> "+line)+"\n"+meta)
		return
	}
	_, _ = fmt.Fprint(w, "  "+line+"\n"+meta)
}

var (
	borderStyle     = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	activeStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Bold(true)
	headerStyle     = lipgloss.NewStyle().Bold(true)
	mutedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	okStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("120")).Bold(true)
	warnStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	badStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	tagStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	statusTextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	hintStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("247"))
	modalStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("214")).Padding(0, 1)
)

func defaultKeyMap() keyMap {
	return keyMap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("up/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("down/j", "down")),
		Backup:    key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "backup")),
		Toggle:    key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "toggle boot")),
		Reconcile: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "reconcile")),
		Recovery:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "recovery cmds")),
		Hook:      key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "install hook")),
		Delete:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Remove:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "remove")),
		Switch:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Confirm:   key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "confirm")),
		Cancel:    key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "cancel")),
	}
}

func newModelComponents() Model {
	keys := defaultKeyMap()
	backupList := list.New(nil, backupDelegate{}, 96, 14)
	backupList.SetShowTitle(false)
	backupList.SetShowFilter(false)
	backupList.SetFilteringEnabled(false)
	backupList.SetShowStatusBar(false)
	backupList.SetShowHelp(false)
	backupList.SetShowPagination(true)
	backupList.DisableQuitKeybindings()

	entryTable := table.New(
		table.WithColumns(entryColumns(96)),
		table.WithRows(nil),
		table.WithFocused(false),
		table.WithHeight(12),
	)
	entryStyles := table.DefaultStyles()
	entryStyles.Header = entryStyles.Header.BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).Bold(true)
	entryStyles.Selected = activeStyle
	entryTable.SetStyles(entryStyles)

	h := help.New()
	h.SetWidth(96)
	return Model{
		mode:          modeBackups,
		keys:          keys,
		backupList:    backupList,
		entryTable:    entryTable,
		activityWidth: 32,
		help:          h,
		width:         96,
		height:        24,
		taskFrame:     0,
		confirmDelete: false,
	}
}

func entryColumns(width int) []table.Column {
	if width < 72 {
		width = 72
	}
	nameWidth := 26
	statusWidth := 12
	pathWidth := width - nameWidth - statusWidth - 10
	if pathWidth < 24 {
		pathWidth = 24
	}
	return []table.Column{
		{Title: "Name", Width: nameWidth},
		{Title: "Status", Width: statusWidth},
		{Title: "Snapshot", Width: pathWidth},
	}
}

// NewModel loads backups and returns a model
func NewModel() (Model, error) {
	if err := CheckRuntimeDependencies(); err != nil {
		return Model{}, err
	}
	if err := ensureGrubFile(); err != nil {
		// proceed even if permissions prevent creation
		if !os.IsPermission(err) {
			return Model{}, err
		}
	}
	b, err := DiscoverBackups()
	if err != nil {
		return Model{}, err
	}
	e, err := ListGrubEntries()
	if err != nil {
		return Model{}, err
	}
	markGrubFlags(b, e)
	m := newModelComponents()
	m.Backups = b
	m.Entries = e
	if countInactiveBackups(m.Backups) > 0 {
		m.status = fmt.Sprintf("snapshots detected without EFI activation. press g to activate selected backup")
	}
	return m.syncComponents(), nil
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles key messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.resizeComponents()
		return m, nil
	case activityTickMsg:
		if m.busy {
			m.taskFrame++
			return m, activityTick()
		}
		return m, nil
	case taskOutputMsg:
		if !m.busy {
			return m, nil
		}
		if !msg.ok {
			return m, nil
		}
		m.taskDetail = msg.line
		return m, waitTaskOutput(m.taskOutput)
	case taskDoneMsg:
		if !m.busy || msg.task != m.task {
			return m, nil
		}
		m.busy = false
		m.task = taskNone
		m.taskLabel = ""
		m.taskDetail = ""
		m.taskOutput = nil
		m.taskFrame = 0
		if msg.errStatus != "" {
			m.status = msg.errStatus
			return m, nil
		}
		if msg.backups != nil {
			m.Backups = msg.backups
		}
		if msg.entries != nil {
			m.Entries = msg.entries
		}
		m.mode = msg.mode
		m.cursor = cursorAfterTask(m, msg)
		m.status = msg.status
		return m.syncComponents(), nil
	case tea.KeyPressMsg:
		if m.busy {
			switch msg.String() {
			case "ctrl+c", "q":
				m.taskDetail = "operation is still running; wait for it to finish before quitting"
				return m, nil
			default:
				return m, nil
			}
		}
		if m.confirmDelete {
			switch msg.String() {
			case "y":
				target := m.deleteTarget
				m.confirmDelete = false
				m.deleteTarget = ""
				return m.startTask(taskDelete, fmt.Sprintf("deleting %s", target), runDeleteTask(target))
			case "n", "esc":
				m.confirmDelete = false
				m.deleteTarget = ""
				m.status = "delete canceled"
				return m, nil
			case "ctrl+c", "q":
				return m, tea.Quit
			default:
				return m, nil
			}
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Switch):
			if m.mode == modeBackups {
				m.mode = modeEntries
				m.cursor = clampCursor(m.cursor, len(m.Entries))
			} else {
				m.mode = modeBackups
				m.cursor = clampCursor(m.cursor, len(m.Backups))
			}
			return m.syncComponents(), nil
		case key.Matches(msg, m.keys.Toggle):
			if m.mode == modeBackups && len(m.Backups) > 0 {
				b := m.Backups[m.cursor]
				if b.HasEFI || b.GrubEntryExists {
					return m.startTask(taskDeactivate, fmt.Sprintf("deactivating %s", b.Name), runToggleBackupTask(taskDeactivate, b.Name))
				}
				return m.startTask(taskActivate, fmt.Sprintf("activating %s", b.Name), runToggleBackupTask(taskActivate, b.Name))
			}
		case key.Matches(msg, m.keys.Reconcile):
			if m.mode == modeBackups {
				return m.startTask(taskReconcile, "reconciling backups and bootloader entries", runReconcileTask)
			}
		case key.Matches(msg, m.keys.Backup):
			if m.mode == modeBackups {
				return m.startTask(taskBackup, "creating snapshot and module archive", runBackupTask)
			}
		case key.Matches(msg, m.keys.Hook):
			if m.mode == modeBackups {
				if HookInstalled() {
					return m.startTask(taskUninstallHook, "uninstalling hooks and rebuilding initramfs (mkinitcpio -P)", runHookToggleTask(taskUninstallHook))
				}
				return m.startTask(taskInstallHook, "installing hooks and rebuilding initramfs (mkinitcpio -P)", runHookToggleTask(taskInstallHook))
			}
		case key.Matches(msg, m.keys.Recovery):
			if m.mode == modeBackups && len(m.Backups) > 0 {
				commands, err := RecoveryCommands(m.Backups[m.cursor].Name)
				if err != nil {
					m.status = fmt.Sprintf("recovery hints unavailable: %v", err)
					break
				}
				m.status = "GRUB recovery commands:\n" + commands
			}
		case key.Matches(msg, m.keys.Remove):
			if m.mode == modeEntries && len(m.Entries) > 0 {
				entry := m.Entries[m.cursor]
				return m.startTask(taskRemoveEntry, fmt.Sprintf("removing bootloader entry %s", entry.Name), runRemoveEntryTask(entry.ID))
			}
		case key.Matches(msg, m.keys.Delete):
			if m.mode == modeBackups && len(m.Backups) > 0 {
				m.confirmDelete = true
				m.deleteTarget = m.Backups[m.cursor].Name
				return m.syncComponents(), nil
			}
		}
		return m.updateFocusedComponent(msg)
	}
	return m, nil
}

func (m Model) startTask(kind taskKind, label string, cmdFactory taskCmdFactory) (tea.Model, tea.Cmd) {
	output := make(chan string, 64)
	m.busy = true
	m.task = kind
	m.taskLabel = label
	m.taskDetail = ""
	m.taskOutput = output
	m.taskFrame = 0
	m.status = label + "..."
	return m.syncComponents(), tea.Batch(cmdFactory(output), activityTick(), waitTaskOutput(output))
}

func activityTick() tea.Cmd {
	return tea.Tick(70*time.Millisecond, func(time.Time) tea.Msg {
		return activityTickMsg{}
	})
}

func waitTaskOutput(output <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-output
		return taskOutputMsg{line: line, ok: ok}
	}
}

func taskCmdWithOutput(output chan<- string, fn func() tea.Msg) tea.Cmd {
	return func() tea.Msg {
		defer close(output)
		return withCommandOutputSink(func(line string) {
			select {
			case output <- line:
			default:
			}
		}, fn)
	}
}

func (m Model) updateFocusedComponent(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.mode {
	case modeEntries:
		m.entryTable, cmd = m.entryTable.Update(msg)
		m.cursor = clampCursor(m.entryTable.Cursor(), len(m.Entries))
	default:
		m.backupList, cmd = m.backupList.Update(msg)
		m.cursor = clampCursor(m.backupList.Index(), len(m.Backups))
	}
	return m.syncComponents(), cmd
}

func (m Model) syncComponents() Model {
	m.cursor = clampCursor(m.cursor, m.activeLen())
	items := make([]list.Item, 0, len(m.Backups))
	for _, b := range m.Backups {
		items = append(items, backupItem{backup: b})
	}
	_ = m.backupList.SetItems(items)
	m.backupList.Select(m.cursor)

	rows := make([]table.Row, 0, len(m.Entries))
	for _, entry := range m.Entries {
		rows = append(rows, table.Row{entry.Name, entryStatus(entry, m.Backups), entry.BackupPath})
	}
	m.entryTable.SetRows(rows)
	m.entryTable.SetCursor(m.cursor)
	if m.mode == modeEntries {
		m.entryTable.Focus()
	} else {
		m.entryTable.Blur()
	}
	return m.resizeComponents()
}

func (m Model) resizeComponents() Model {
	width := m.width
	if width <= 0 {
		width = 96
	}
	height := m.height
	if height <= 0 {
		height = 24
	}
	innerWidth := width - 6
	if innerWidth < 60 {
		innerWidth = 60
	}
	listHeight := compactBackupListHeight(len(m.Backups), height)
	tableHeight := compactEntryTableHeight(len(m.Entries), height)
	m.backupList.SetSize(innerWidth, listHeight)
	m.entryTable.SetColumns(entryColumns(innerWidth))
	m.entryTable.SetWidth(innerWidth)
	m.entryTable.SetHeight(tableHeight)
	m.activityWidth = innerWidth
	m.help.SetWidth(innerWidth)
	return m
}

func (m Model) activeLen() int {
	if m.mode == modeEntries {
		return len(m.Entries)
	}
	return len(m.Backups)
}

func compactBackupListHeight(count, terminalHeight int) int {
	if count <= 0 {
		return 4
	}
	available := terminalHeight - 10
	if available < 5 {
		available = 5
	}
	maxHeight := min(14, available)
	needed := count*3 + 1
	return clampSize(needed, 5, maxHeight)
}

func compactEntryTableHeight(count, terminalHeight int) int {
	if count <= 0 {
		return 4
	}
	available := terminalHeight - 11
	if available < 4 {
		available = 4
	}
	maxHeight := min(10, available)
	needed := count + 2
	return clampSize(needed, 4, maxHeight)
}

func clampSize(value, minValue, maxValue int) int {
	if maxValue < minValue {
		return minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func cursorAfterTask(m Model, msg taskDoneMsg) int {
	switch msg.mode {
	case modeEntries:
		if msg.cursorEntryID != "" {
			for i, entry := range m.Entries {
				if entry.ID == msg.cursorEntryID {
					return i
				}
			}
		}
		return clampCursor(m.cursor, len(m.Entries))
	default:
		if msg.cursorName != "" {
			for i, b := range m.Backups {
				if b.Name == msg.cursorName {
					return i
				}
			}
		}
		return clampCursor(m.cursor, len(m.Backups))
	}
}

func runBackupTask(output chan<- string) tea.Cmd {
	return taskCmdWithOutput(output, func() tea.Msg {
		created, err := CreateBootBackupNow()
		if err != nil {
			return taskDoneMsg{task: taskBackup, mode: modeBackups, errStatus: fmt.Sprintf("backup failed: %v", err)}
		}
		backups, entries, errStatus := refreshTaskState()
		if errStatus != "" {
			return taskDoneMsg{task: taskBackup, mode: modeBackups, errStatus: errStatus}
		}
		return taskDoneMsg{
			task:       taskBackup,
			mode:       modeBackups,
			backups:    backups,
			entries:    entries,
			cursorName: created.Name,
			status:     fmt.Sprintf("snapshot created: %s (press g to activate in EFI+GRUB)", filepath.Base(created.Path)),
		}
	})
}

func runToggleBackupTask(kind taskKind, name string) taskCmdFactory {
	return func(output chan<- string) tea.Cmd {
		return taskCmdWithOutput(output, func() tea.Msg {
			if kind == taskDeactivate {
				if err := DeactivateBackup(name); err != nil {
					return taskDoneMsg{task: kind, mode: modeBackups, errStatus: fmt.Sprintf("deactivate failed: %v", err)}
				}
			} else {
				if err := ActivateBackup(name); err != nil {
					return taskDoneMsg{task: kind, mode: modeBackups, errStatus: fmt.Sprintf("activate failed: %v", err)}
				}
			}
			backups, entries, errStatus := refreshTaskState()
			if errStatus != "" {
				return taskDoneMsg{task: kind, mode: modeBackups, errStatus: errStatus}
			}
			verb := "activated"
			if kind == taskDeactivate {
				verb = "deactivated"
			}
			return taskDoneMsg{task: kind, mode: modeBackups, backups: backups, entries: entries, cursorName: name, status: fmt.Sprintf("%s: %s", verb, name)}
		})
	}
}

func runReconcileTask(output chan<- string) tea.Cmd {
	return taskCmdWithOutput(output, func() tea.Msg {
		backups, entries, err := SyncBackupsAndGrub()
		if err != nil {
			return taskDoneMsg{task: taskReconcile, mode: modeBackups, errStatus: fmt.Sprintf("sync failed: %v", err)}
		}
		return taskDoneMsg{
			task:    taskReconcile,
			mode:    modeBackups,
			backups: backups,
			entries: entries,
			status:  fmt.Sprintf("reconcile complete. active EFI mirrors refreshed and %s cleaned", GrubCustom),
		}
	})
}

func runDeleteTask(name string) taskCmdFactory {
	return func(output chan<- string) tea.Cmd {
		return taskCmdWithOutput(output, func() tea.Msg {
			if err := DeleteBackup(name); err != nil {
				return taskDoneMsg{task: taskDelete, mode: modeBackups, errStatus: fmt.Sprintf("delete failed: %v", err)}
			}
			backups, entries, errStatus := refreshTaskState()
			if errStatus != "" {
				return taskDoneMsg{task: taskDelete, mode: modeBackups, errStatus: errStatus}
			}
			return taskDoneMsg{task: taskDelete, mode: modeBackups, backups: backups, entries: entries, status: fmt.Sprintf("backup deleted: %s", name)}
		})
	}
}

func runHookToggleTask(kind taskKind) taskCmdFactory {
	return func(output chan<- string) tea.Cmd {
		return taskCmdWithOutput(output, func() tea.Msg {
			if kind == taskUninstallHook {
				removed, err := UninstallPacmanHook()
				if err != nil {
					return taskDoneMsg{task: kind, mode: modeBackups, errStatus: fmt.Sprintf("hook uninstall failed: %v", err)}
				}
				status := "package-manager and initramfs hooks are not installed"
				if removed {
					status = "package-manager and initramfs hooks uninstalled"
				}
				return taskDoneMsg{task: kind, mode: modeBackups, status: status}
			}
			if err := InstallPacmanHook(defaultHookExecutablePath()); err != nil {
				return taskDoneMsg{task: kind, mode: modeBackups, errStatus: fmt.Sprintf("hook install failed: %v", err)}
			}
			return taskDoneMsg{task: kind, mode: modeBackups, status: fmt.Sprintf("package-manager and initramfs hooks installed: %s, %s", PacmanHookPath, MkinitcpioHookPath)}
		})
	}
}

func runRemoveEntryTask(id string) taskCmdFactory {
	return func(output chan<- string) tea.Cmd {
		return taskCmdWithOutput(output, func() tea.Msg {
			if err := RemoveGrubEntry(id); err != nil {
				return taskDoneMsg{task: taskRemoveEntry, mode: modeEntries, errStatus: fmt.Sprintf("remove failed: %v", err)}
			}
			backups, entries, errStatus := refreshTaskState()
			if errStatus != "" {
				return taskDoneMsg{task: taskRemoveEntry, mode: modeEntries, errStatus: errStatus}
			}
			return taskDoneMsg{task: taskRemoveEntry, mode: modeEntries, backups: backups, entries: entries, status: "entry removed"}
		})
	}
}

func refreshTaskState() ([]BootBackup, []GrubEntry, string) {
	backups, backupsErr := DiscoverBackups()
	entries, entriesErr := ListGrubEntries()
	if backupsErr != nil {
		return nil, nil, fmt.Sprintf("refresh failed: %v", backupsErr)
	}
	if entriesErr != nil {
		return nil, nil, fmt.Sprintf("refresh failed: %v", entriesErr)
	}
	markGrubFlags(backups, entries)
	return backups, entries, ""
}

// View renders the TUI.
func (m Model) View() tea.View {
	return tea.NewView(m.viewString())
}

func (m Model) viewString() string {
	if m.mode == modeEntries {
		return m.viewEntries()
	}
	return m.viewBackups()
}

func (m Model) viewBackups() string {
	var sections []string
	sections = append(sections, m.tabs())
	if len(m.Backups) == 0 {
		sections = append(sections, mutedStyle.Render("No backups found"))
	} else {
		sections = append(sections, m.backupList.View())
	}
	sections = append(sections, taskDetailLine(m))
	sections = append(sections, statusActivityLine(m))
	sections = append(sections, m.footer())
	header := headerStyle.Render("Backups") + "  " + mutedStyle.Render(backupCapacitySummary()) + "  " + hookStateBadge()
	return borderStyle.Render(header + "\n" + strings.Join(sections, "\n"))
}

func (m Model) viewEntries() string {
	sections := []string{
		m.tabs(),
		mutedStyle.Render("source: " + GrubCustom),
	}
	if len(m.Entries) == 0 {
		sections = append(sections, mutedStyle.Render("None"))
	} else {
		sections = append(sections, m.entryTable.View())
	}
	sections = append(sections, taskDetailLine(m))
	sections = append(sections, statusActivityLine(m))
	sections = append(sections, m.footer())
	return borderStyle.Render(headerStyle.Render("Bootloader Entries") + "  " + hookStateBadge() + "\n" + strings.Join(sections, "\n"))
}

func taskDetailLine(m Model) string {
	if !m.busy || strings.TrimSpace(m.taskDetail) == "" {
		return ""
	}
	return hintStyle.Render(truncateVisible(m.taskDetail, m.activityWidth))
}

func statusActivityLine(m Model) string {
	if m.busy {
		label := m.taskLabel
		if label == "" {
			label = "working"
		}
		return activityLineWithText(m.activityWidth, label, func(width int) string {
			return activityBar(m.taskFrame, width)
		}, mutedStyle)
	}
	if strings.TrimSpace(m.status) == "" {
		return ""
	}
	return activityLineWithText(m.activityWidth, m.status, completeActivityBar, statusTextStyle)
}

func activityLineWithText(width int, text string, bar func(int) string, textStyle lipgloss.Style) string {
	if width < 20 {
		width = 20
	}
	const (
		gapWidth = 2
		minBar   = 12
	)
	text = strings.TrimSpace(text)
	maxTextWidth := width - minBar - gapWidth
	if maxTextWidth < 1 {
		maxTextWidth = 1
	}
	text = truncateVisible(text, maxTextWidth)
	renderedText := textStyle.Render(text)
	textWidth := lipgloss.Width(renderedText)
	barWidth := width - gapWidth - textWidth
	if barWidth < minBar {
		barWidth = minBar
	}
	gap := strings.Repeat(" ", gapWidth)
	return bar(barWidth) + gap + renderedText
}

func activityBar(frame, width int) string {
	if width < 12 {
		width = 12
	}
	segmentWidth := width / 4
	if segmentWidth < 5 {
		segmentWidth = 5
	}
	trackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	segmentStyles := activitySegmentStyles()
	position := frame % (width + segmentWidth)
	start := position - segmentWidth
	var b strings.Builder
	b.Grow(width)
	for i := 0; i < width; i++ {
		if i >= start && i < start+segmentWidth {
			segmentIndex := i - start
			style := segmentStyles[(segmentIndex*len(segmentStyles))/segmentWidth]
			b.WriteString(style.Render("━"))
			continue
		}
		b.WriteString(trackStyle.Render("─"))
	}
	return b.String()
}

func completeActivityBar(width int) string {
	if width < 12 {
		width = 12
	}
	segmentStyles := activitySegmentStyles()
	var b strings.Builder
	b.Grow(width)
	for i := 0; i < width; i++ {
		style := segmentStyles[(i*len(segmentStyles))/width]
		b.WriteString(style.Render("━"))
	}
	return b.String()
}

func truncateVisible(text string, width int) string {
	if width <= 0 || lipgloss.Width(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func activitySegmentStyles() []lipgloss.Style {
	return []lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color("#21A67A")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#2FAE9A")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#3AA7C9")),
	}
}

func (m Model) tabs() string {
	backups := "Backups"
	entries := "Bootloader"
	if m.mode == modeBackups {
		backups = activeStyle.Render(backups)
		entries = mutedStyle.Render(entries)
	} else {
		backups = mutedStyle.Render(backups)
		entries = activeStyle.Render(entries)
	}
	return backups + mutedStyle.Render(" | ") + entries
}

func (m Model) footer() string {
	if m.busy {
		return hintStyle.Render("working... wait until the operation finishes")
	}
	if m.confirmDelete {
		return modalStyle.Render(fmt.Sprintf("Delete backup '%s'? y=yes, n=no", m.deleteTarget))
	}
	return hintStyle.Render(m.help.View(m.viewHelpKeyMap()))
}

func (m Model) viewHelpKeyMap() helpKeyMap {
	hook := m.keys.Hook
	if HookInstalled() {
		hook.SetHelp("p", "uninstall hook")
	} else {
		hook.SetHelp("p", "install hook")
	}
	if m.mode == modeEntries {
		return helpKeyMap{
			short: []key.Binding{m.keys.Up, m.keys.Down, m.keys.Remove, m.keys.Switch, m.keys.Help, m.keys.Quit},
			full: [][]key.Binding{
				{m.keys.Up, m.keys.Down, m.keys.Remove},
				{m.keys.Switch, m.keys.Help, m.keys.Quit},
			},
		}
	}
	return helpKeyMap{
		short: []key.Binding{m.keys.Up, m.keys.Down, m.keys.Backup, m.keys.Toggle, hook, m.keys.Delete, m.keys.Switch, m.keys.Help, m.keys.Quit},
		full: [][]key.Binding{
			{m.keys.Up, m.keys.Down, m.keys.Backup, m.keys.Toggle},
			{m.keys.Reconcile, m.keys.Recovery, hook, m.keys.Delete},
			{m.keys.Switch, m.keys.Help, m.keys.Quit},
		},
	}
}

func hookStateBadge() string {
	if HookInstalled() {
		return okStyle.Render("Hook: ON")
	}
	return mutedStyle.Render("Hook: OFF")
}

func entryStatus(entry GrubEntry, backups []BootBackup) string {
	for _, b := range backups {
		if b.Name == entry.Name || b.SnapshotPath == entry.BackupPath || b.EFIPath == entry.BackupPath {
			return statusString(b)
		}
	}
	return "active"
}

func statusString(b BootBackup) string {
	if !b.HasSnapshot {
		return "Missing"
	}
	if b.HasKernel && b.HasInitramfs {
		if IsRestoreReady(b) {
			return "Restore"
		}
		if hasKnownMissingRootModules(b) {
			return "No modules"
		}
		return "OK"
	}
	return "Incomplete"
}

func statusBadge(b BootBackup) string {
	switch statusString(b) {
	case "OK":
		return okStyle.Render("[OK]")
	case "Restore":
		return warnStyle.Render("[RESTORE]")
	case "Missing":
		return badStyle.Render("[MISSING]")
	default:
		return warnStyle.Render("[INCOMPLETE]")
	}
}

func countInactiveBackups(backups []BootBackup) int {
	count := 0
	for _, b := range backups {
		if b.HasSnapshot && !b.HasEFI {
			count++
		}
	}
	return count
}

func backupMetaSummary(b BootBackup) string {
	kernel := b.KernelVersion
	if kernel == "" {
		kernel = "unknown"
	}
	date := "unknown-date"
	if !b.CreatedAt.IsZero() {
		date = b.CreatedAt.Local().Format("2006-01-02 15:04")
	}
	size := humanSize(b.SizeBytes)
	micro := "none"
	if len(b.MicrocodeImages) > 0 {
		micro = strings.Join(b.MicrocodeImages, ",")
	}
	entry := "no"
	if b.GrubEntryExists {
		entry = "yes"
	}
	efi := "off"
	if b.HasEFI {
		efi = "on"
	}
	bootable := "no"
	if IsBootReady(b) {
		bootable = "yes"
	}
	return fmt.Sprintf("[ver:%s backup:%s size:%s efi:%s bootable:%s grub:%s modules:%s ucode:%s]", kernel, date, size, efi, bootable, entry, rootModuleStatus(b), micro)
}

func humanSize(bytes int64) string {
	const (
		ki = int64(1024)
		mi = ki * 1024
		gi = mi * 1024
	)
	switch {
	case bytes >= gi:
		return fmt.Sprintf("%.1fGiB", float64(bytes)/float64(gi))
	case bytes >= mi:
		return fmt.Sprintf("%.1fMiB", float64(bytes)/float64(mi))
	case bytes >= ki:
		return fmt.Sprintf("%.1fKiB", float64(bytes)/float64(ki))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func clampCursor(cursor, listLen int) int {
	if listLen <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= listLen {
		return listLen - 1
	}
	return cursor
}
