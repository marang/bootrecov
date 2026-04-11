package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/marang/bootrecov/internal/tui"
)

func main() {
	if v := strings.TrimSpace(os.Getenv("BOOTRECOV_BACKUP_PROFILE")); v != "" {
		tui.BackupProfile = v
	}
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "backup-now":
			created, err := tui.CreateBootBackupNow()
			if err != nil {
				return err
			}
			fmt.Println(created.Name)
			return nil
		case "install-pacman-hook":
			execPath := ""
			if len(args) > 1 {
				execPath = args[1]
			}
			return tui.InstallPacmanHook(execPath)
		case "recovery-commands":
			if len(args) < 2 {
				return fmt.Errorf("usage: bootrecov recovery-commands <snapshot-name>")
			}
			commands, err := tui.RecoveryCommands(args[1])
			if err != nil {
				return err
			}
			fmt.Println(commands)
			return nil
		case "help", "--help", "-h":
			fmt.Println("usage: bootrecov [backup-now|install-pacman-hook [path]|recovery-commands <snapshot-name>]")
			return nil
		}
	}
	m, err := tui.NewModel()
	if err != nil {
		return err
	}
	if err := tea.NewProgram(m).Start(); err != nil {
		return err
	}
	return nil
}
