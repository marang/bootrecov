package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/marang/bootrecov/internal/tui"
)

func main() {
	configureFromEnv()
	if err := newRootCmd().Execute(); err != nil {
		log.Fatal(err)
	}
}

func configureFromEnv() {
	if v := strings.TrimSpace(os.Getenv("BOOTRECOV_BACKUP_PROFILE")); v != "" {
		tui.BackupProfile = v
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "bootrecov",
		Short:         "Manage /boot recovery snapshots and GRUB fallback entries",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}

	commands := []*cobra.Command{
		newTUICmd(),
		newReconcileCmd(),
		newHookCmd(),
		newGrubCmd(),
		newBackupCmd(),
	}
	commands = append(commands, newCompatibilityCmds()...)
	rootCmd.AddCommand(commands...)
	return rootCmd
}

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Start the interactive TUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}
}

func newReconcileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Reconcile EFI mirrors and GRUB recovery entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			backups, entries, err := tui.SyncBackupsAndGrub()
			if err != nil {
				return err
			}
			fmt.Printf("reconciled %d backups and %d GRUB entries\n", len(backups), len(entries))
			return nil
		},
	}
}

func newHookCmd() *cobra.Command {
	hookCmd := &cobra.Command{
		Use:   "hook",
		Short: "Manage pacman hook integration",
	}
	installCmd := &cobra.Command{
		Use:   "install [absolute-binary-path]",
		Short: "Install or refresh the pacman pre-transaction hook",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) == 1 {
				path = args[0]
			}
			if err := tui.InstallPacmanHook(path); err != nil {
				return err
			}
			fmt.Printf("installed pacman hook at %s\n", tui.PacmanHookPath)
			return nil
		},
	}
	hookCmd.AddCommand(installCmd)
	return hookCmd
}

func newGrubCmd() *cobra.Command {
	grubCmd := &cobra.Command{
		Use:   "grub",
		Short: "Inspect GRUB recovery entries",
	}
	grubCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List Bootrecov GRUB entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := tui.ListGrubEntries()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println("no GRUB entries found")
				return nil
			}
			tw := newTabWriter()
			fmt.Fprintln(tw, "ID\tNAME\tPATH")
			for _, entry := range entries {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", entry.ID, entry.Name, entry.BackupPath)
			}
			return tw.Flush()
		},
	})
	return grubCmd
}

func newBackupCmd() *cobra.Command {
	backupCmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage stored /boot recovery snapshots",
	}

	backupCmd.AddCommand(
		&cobra.Command{
			Use:     "create",
			Aliases: []string{"new"},
			Short:   "Create a new /boot snapshot",
			RunE: func(cmd *cobra.Command, args []string) error {
				created, err := tui.CreateBootBackupNow()
				if err != nil {
					return err
				}
				fmt.Println(created.Name)
				return nil
			},
		},
		&cobra.Command{
			Use:   "list",
			Short: "List discovered snapshots and activation state",
			RunE: func(cmd *cobra.Command, args []string) error {
				backups, entries, err := tui.RefreshBackupsAndGrub()
				if err != nil {
					return err
				}
				if len(backups) == 0 {
					fmt.Println("no backups found")
					return nil
				}
				_ = entries
				tw := newTabWriter()
				fmt.Fprintln(tw, "NAME\tSNAPSHOT\tEFI\tGRUB\tBOOTABLE\tROOT-MODULES\tCREATED\tSIZE\tKERNEL")
				for _, b := range backups {
					fmt.Fprintf(
						tw,
						"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
						b.Name,
						boolWord(b.HasSnapshot),
						boolWord(b.HasEFI),
						boolWord(b.GrubEntryExists),
						boolWord(isBootable(b)),
						rootModulesWord(b),
						formatTime(b.CreatedAt),
						formatBytesCLI(b.SizeBytes),
						formatKernel(b.KernelVersion),
					)
				}
				return tw.Flush()
			},
		},
		&cobra.Command{
			Use:   "activate <snapshot-name>",
			Short: "Activate a snapshot for EFI + GRUB booting",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := tui.ActivateBackup(args[0]); err != nil {
					return err
				}
				fmt.Printf("activated %s\n", args[0])
				return nil
			},
		},
		&cobra.Command{
			Use:   "deactivate <snapshot-name>",
			Short: "Deactivate a snapshot and remove its EFI mirror and GRUB entry",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := tui.DeactivateBackup(args[0]); err != nil {
					return err
				}
				fmt.Printf("deactivated %s\n", args[0])
				return nil
			},
		},
		&cobra.Command{
			Use:   "delete <snapshot-name>",
			Short: "Delete a snapshot and its related EFI/GRUB artifacts",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := tui.DeleteBackup(args[0]); err != nil {
					return err
				}
				fmt.Printf("deleted %s\n", args[0])
				return nil
			},
		},
		&cobra.Command{
			Use:   "recovery <snapshot-name>",
			Short: "Print GRUB recovery commands for an activated snapshot",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				commands, err := tui.RecoveryCommands(args[0])
				if err != nil {
					return err
				}
				fmt.Println(commands)
				return nil
			},
		},
	)
	return backupCmd
}

func newCompatibilityCmds() []*cobra.Command {
	return []*cobra.Command{
		{
			Use:    "backup-now",
			Hidden: true,
			Short:  "Compatibility alias for backup create",
			RunE: func(cmd *cobra.Command, args []string) error {
				created, err := tui.CreateBootBackupNow()
				if err != nil {
					return err
				}
				fmt.Println(created.Name)
				return nil
			},
		},
		{
			Use:    "install-pacman-hook [absolute-binary-path]",
			Hidden: true,
			Short:  "Compatibility alias for hook install",
			Args:   cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				path := ""
				if len(args) == 1 {
					path = args[0]
				}
				return tui.InstallPacmanHook(path)
			},
		},
		{
			Use:    "recovery-commands <snapshot-name>",
			Hidden: true,
			Short:  "Compatibility alias for backup recovery",
			Args:   cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				commands, err := tui.RecoveryCommands(args[0])
				if err != nil {
					return err
				}
				fmt.Println(commands)
				return nil
			},
		},
	}
}

func runTUI() error {
	m, err := tui.NewModel()
	if err != nil {
		return err
	}
	if err := tea.NewProgram(m).Start(); err != nil {
		return err
	}
	return nil
}

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
}

func boolWord(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func isBootable(b tui.BootBackup) bool {
	return tui.IsBootReady(b)
}

func rootModulesWord(b tui.BootBackup) string {
	if !b.RootModulesKnown {
		return "unknown"
	}
	if b.HasRootModules {
		return "yes"
	}
	if b.HasArchivedModules {
		return "archived"
	}
	return "missing"
}

func formatTime(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	return ts.Local().Format("2006-01-02 15:04")
}

func formatKernel(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func formatBytesCLI(bytes int64) string {
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
	case bytes > 0:
		return fmt.Sprintf("%dB", bytes)
	default:
		return "-"
	}
}
