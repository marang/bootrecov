package tui

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model contains application state

type Model struct {
	Backups []BootBackup
	Entries []string
	cursor  int
	mode    mode
}

type mode int

const (
	modeBackups mode = iota
	modeEntries
)

var (
	borderStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	activeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("63"))
	headerStyle = lipgloss.NewStyle().Bold(true)
)

// NewModel loads backups and returns a model
func NewModel() (Model, error) {
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
	return Model{Backups: b, Entries: e, mode: modeBackups}, nil
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles key messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			if m.mode == modeBackups {
				m.mode = modeEntries
				if m.cursor >= len(m.Entries) {
					m.cursor = len(m.Entries) - 1
				}
			} else {
				m.mode = modeBackups
				if m.cursor >= len(m.Backups) {
					m.cursor = len(m.Backups) - 1
				}
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
				if b.GrubEntryExists {
					RemoveGrubEntry(filepath.Base(b.Path))
					b.GrubEntryExists = false
				} else {
					AddGrubEntry(b)
					b.GrubEntryExists = true
				}
				m.Backups[m.cursor] = b
			}
		case "x":
			if m.mode == modeEntries && len(m.Entries) > 0 {
				name := m.Entries[m.cursor]
				if err := RemoveGrubEntry(name); err == nil {
					m.Entries = append(m.Entries[:m.cursor], m.Entries[m.cursor+1:]...)
					if m.cursor >= len(m.Entries) && m.cursor > 0 {
						m.cursor--
					}
					for i, b := range m.Backups {
						if filepath.Base(b.Path) == name {
							b.GrubEntryExists = false
							m.Backups[i] = b
						}
					}
				}
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
	if len(m.Backups) == 0 {
		return "No backups found\n"
	}
	var s string
	for i, b := range m.Backups {
		line := fmt.Sprintf("%s %s", filepath.Base(b.Path), statusString(b))
		if b.GrubEntryExists {
			line += " [grub]"
		}
		if m.cursor == i {
			s += activeStyle.Render(line) + "\n"
		} else {
			s += line + "\n"
		}
	}
	return borderStyle.Render(headerStyle.Render("Backups") + "\n" + s + "q: quit, g: toggle grub entry, tab: entries")
}

func (m Model) viewEntries() string {
	if len(m.Entries) == 0 {
		return borderStyle.Render(headerStyle.Render("GRUB Entries") + "\nNone\nq: quit, tab: backups")
	}
	var s string
	for i, name := range m.Entries {
		if m.cursor == i {
			s += activeStyle.Render(name) + "\n"
		} else {
			s += name + "\n"
		}
	}
	return borderStyle.Render(headerStyle.Render("GRUB Entries") + "\n" + s + "q: quit, x: remove, tab: backups")
}

func statusString(b BootBackup) string {
	if b.HasKernel && b.HasInitramfs {
		return "OK"
	}
	return "Incomplete"
}
