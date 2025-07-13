package tui

import (
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// Model contains application state

type Model struct {
	Backups []BootBackup
	cursor  int
}

// NewModel loads backups and returns a model
func NewModel() (Model, error) {
	b, err := DiscoverBackups()
	if err != nil {
		return Model{}, err
	}
	return Model{Backups: b}, nil
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
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.Backups)-1 {
				m.cursor++
			}
		case "g":
			if len(m.Backups) > 0 {
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
		}
	}
	return m, nil
}

// View renders the list of backups
func (m Model) View() string {
	if len(m.Backups) == 0 {
		return "No backups found\n"
	}
	s := "Boot backups:\n"
	for i, b := range m.Backups {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}
		status := "OK"
		if !b.HasKernel || !b.HasInitramfs {
			status = "Incomplete"
		}
		grub := ""
		if b.GrubEntryExists {
			grub = "[grub]"
		}
		s += fmt.Sprintf("%s %s %s %s\n", cursor, filepath.Base(b.Path), status, grub)
	}
	s += "\nq: quit, g: toggle grub entry"
	return s
}
