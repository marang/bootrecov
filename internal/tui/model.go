package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
}

type mode int

const (
	modeBackups mode = iota
	modeEntries
)

var (
	borderStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Bold(true)
	headerStyle = lipgloss.NewStyle().Bold(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("120")).Bold(true)
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	badStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	tagStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("62")).Padding(0, 1)
	hintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("247"))
	modalStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("214")).Padding(0, 1)
)

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
	m := Model{Backups: b, Entries: e, mode: modeBackups}
	if countInactiveBackups(m.Backups) > 0 {
		m.status = fmt.Sprintf("snapshots detected without EFI activation. press g to activate selected backup")
	}
	return m, nil
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles key messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.confirmDelete {
			switch msg.String() {
			case "y":
				target := m.deleteTarget
				m.confirmDelete = false
				m.deleteTarget = ""
				if err := DeleteBackup(target); err != nil {
					m.status = fmt.Sprintf("delete failed: %v", err)
					return m, nil
				}
				backups, entries, err := RefreshBackupsAndGrub()
				if err != nil {
					m.status = fmt.Sprintf("refresh failed: %v", err)
					return m, nil
				}
				m.Backups = backups
				m.Entries = entries
				m.cursor = clampCursor(m.cursor, len(m.Backups))
				m.status = fmt.Sprintf("backup deleted: %s", target)
				return m, nil
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
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			if m.mode == modeBackups {
				m.mode = modeEntries
				m.cursor = clampCursor(m.cursor, len(m.Entries))
			} else {
				m.mode = modeBackups
				m.cursor = clampCursor(m.cursor, len(m.Backups))
			}
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.mode == modeBackups {
				if m.cursor < len(m.Backups)-1 {
					m.cursor++
				}
			} else {
				if m.cursor < len(m.Entries)-1 {
					m.cursor++
				}
			}
		case "g":
			if m.mode == modeBackups && len(m.Backups) > 0 {
				b := m.Backups[m.cursor]
				if b.HasEFI || b.GrubEntryExists {
					if err := DeactivateBackup(b.Name); err != nil {
						m.status = fmt.Sprintf("deactivate failed: %v", err)
						break
					}
					m.status = fmt.Sprintf("deactivated: %s", b.Name)
				} else {
					if err := ActivateBackup(b.Name); err != nil {
						m.status = fmt.Sprintf("activate failed: %v", err)
						break
					}
					m.status = fmt.Sprintf("activated: %s", b.Name)
				}
				backups, entries, err := RefreshBackupsAndGrub()
				if err != nil {
					m.status = fmt.Sprintf("refresh failed: %v", err)
					break
				}
				m.Backups = backups
				m.Entries = entries
				m.cursor = clampCursor(m.cursor, len(m.Backups))
			}
		case "s":
			if m.mode == modeBackups {
				backups, entries, err := SyncBackupsAndGrub()
				if err != nil {
					m.status = fmt.Sprintf("sync failed: %v", err)
					break
				}
				m.Backups = backups
				m.Entries = entries
				m.cursor = clampCursor(m.cursor, len(m.Backups))
				m.status = fmt.Sprintf("reconcile complete. active EFI mirrors refreshed and %s cleaned", GrubCustom)
			}
		case "b":
			if m.mode == modeBackups {
				created, err := CreateBootBackupNow()
				if err != nil {
					m.status = fmt.Sprintf("backup failed: %v", err)
					break
				}
				backups, backupsErr := DiscoverBackups()
				entries, entriesErr := ListGrubEntries()
				if backupsErr != nil || entriesErr != nil {
					if backupsErr != nil {
						m.status = fmt.Sprintf("refresh failed: %v", backupsErr)
					} else {
						m.status = fmt.Sprintf("refresh failed: %v", entriesErr)
					}
					break
				}
				m.Backups = backups
				m.Entries = entries
				for i := range m.Backups {
					if m.Backups[i].Name == created.Name {
						m.cursor = i
						break
					}
				}
				m.status = fmt.Sprintf("snapshot created: %s (press g to activate in EFI+GRUB)", filepath.Base(created.Path))
			}
		case "p":
			if m.mode == modeBackups {
				if err := InstallPacmanHook(defaultHookExecutablePath()); err != nil {
					m.status = fmt.Sprintf("hook install failed: %v", err)
					break
				}
				m.status = fmt.Sprintf("pacman hook installed: %s", PacmanHookPath)
			}
		case "r":
			if m.mode == modeBackups && len(m.Backups) > 0 {
				commands, err := RecoveryCommands(m.Backups[m.cursor].Name)
				if err != nil {
					m.status = fmt.Sprintf("recovery hints unavailable: %v", err)
					break
				}
				m.status = "GRUB recovery commands:\n" + commands
			}
		case "x":
			if m.mode == modeEntries && len(m.Entries) > 0 {
				entry := m.Entries[m.cursor]
				if err := RemoveGrubEntry(entry.ID); err == nil {
					if entries, listErr := ListGrubEntries(); listErr == nil {
						m.Entries = entries
					}
					m.cursor = clampCursor(m.cursor, len(m.Entries))
					m.status = "entry removed"
					for i, b := range m.Backups {
						if backupIDForName(b.Name) == entry.ID {
							b.GrubEntryExists = false
							m.Backups[i] = b
						}
					}
				} else {
					m.status = fmt.Sprintf("remove failed: %v", err)
				}
			}
		case "d":
			if m.mode == modeBackups && len(m.Backups) > 0 {
				m.confirmDelete = true
				m.deleteTarget = m.Backups[m.cursor].Name
			}
		}
	}
	return m, nil
}

// View renders the list of backups
func (m Model) View() string {
	if m.mode == modeEntries {
		return m.viewEntries()
	}
	return m.viewBackups()
}

func (m Model) viewBackups() string {
	var s string
	if len(m.Backups) == 0 {
		s = mutedStyle.Render("No backups found") + "\n"
	} else {
		for i, b := range m.Backups {
			line := fmt.Sprintf("%s %s", b.Name, statusBadge(b))
			if b.GrubEntryExists {
				line += " " + tagStyle.Render("[grub]")
			}
			meta := mutedStyle.Render("  " + backupMetaSummary(b))
			block := line + "\n" + meta
			if m.cursor == i {
				s += activeStyle.Render("> "+line) + "\n" + mutedStyle.Render("  "+backupMetaSummary(b)) + "\n\n"
			} else {
				s += "  " + block + "\n\n"
			}
		}
	}
	if m.status != "" {
		if s != "" {
			s += "\n"
		}
		s += statusStyle.Render(m.status) + "\n"
	}
	footer := "q: quit, b: backup now, g: toggle EFI+GRUB, s: reconcile, r: recovery cmds, p: install pacman hook, d: delete, tab: entries"
	if m.confirmDelete {
		footer = modalStyle.Render(fmt.Sprintf("Delete backup '%s'? y=yes, n=no", m.deleteTarget))
	} else {
		footer = hintStyle.Render(footer)
	}
	header := headerStyle.Render("Backups") + "  " + mutedStyle.Render(backupCapacitySummary())
	return borderStyle.Render(header + "\n" + s + footer)
}

func (m Model) viewEntries() string {
	if len(m.Entries) == 0 {
		return borderStyle.Render(headerStyle.Render("GRUB Entries") + "\nsource: " + GrubCustom + "\nNone\nq: quit, tab: backups")
	}
	var s string
	for i, entry := range m.Entries {
		line := fmt.Sprintf("%s <- %s", entry.Name, entry.BackupPath)
		if m.cursor == i {
			s += activeStyle.Render("> "+line) + "\n"
		} else {
			s += "  " + line + "\n"
		}
	}
	return borderStyle.Render(headerStyle.Render("GRUB Entries") + "\nsource: " + GrubCustom + "\n" + s + "q: quit, x: remove, tab: backups")
}

func statusString(b BootBackup) string {
	if !b.HasSnapshot {
		return "Missing"
	}
	if hasKnownMissingRootModules(b) {
		return "No modules"
	}
	if b.HasKernel && b.HasInitramfs {
		return "OK"
	}
	return "Incomplete"
}

func statusBadge(b BootBackup) string {
	switch statusString(b) {
	case "OK":
		return okStyle.Render("[OK]")
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
