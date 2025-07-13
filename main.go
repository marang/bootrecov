package main

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/marang/bootrecov/tui"
)

func main() {
	m, err := tui.NewModel()
	if err != nil {
		log.Fatal(err)
	}
	if err := tea.NewProgram(m).Start(); err != nil {
		log.Fatal(err)
	}
}
