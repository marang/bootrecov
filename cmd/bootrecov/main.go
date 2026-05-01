package main

import (
	"bufio"
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

const (
	riskAcceptEnv     = "BOOTRECOV_ACCEPT_RISK"
	riskAcceptPhrase  = "I UNDERSTAND"
	riskAcceptFlagMsg = "acknowledge that bootrecov has no warranty and is used at your own risk"
)

var riskAccepted bool

func main() {
	configureFromEnv()
	if err := newRootCmd().Execute(); err != nil {
		log.Fatal(err)
	}
}

func configureFromEnv() {
	tui.ApplyEnvironmentOverridesFromEnv()
	tui.ConfigureDetectedEnvironment()
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "bootrecov",
		Short:         "Manage /boot recovery snapshots and bootloader fallback entries",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return requireRiskAcknowledgement()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}
	rootCmd.PersistentFlags().BoolVar(&riskAccepted, "yes-i-understand", false, riskAcceptFlagMsg)

	commands := []*cobra.Command{
		newTUICmd(),
		newDoctorCmd(),
		newReconcileCmd(),
		newHookCmd(),
		newBootloaderCmd(),
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
		Short: "Reconcile EFI mirrors and bootloader recovery entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			backups, entries, err := tui.SyncBackupsAndGrub()
			if err != nil {
				return err
			}
			fmt.Printf("reconciled %d backups and %d bootloader entries\n", len(backups), len(entries))
			return nil
		},
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Show detected platform, bootloader, paths, and support status",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := tui.CurrentRuntimeEnvironment()
			tw := newTabWriter()
			fmt.Fprintln(tw, "KEY\tVALUE")
			fmt.Fprintf(tw, "platform\t%s (%s)\n", info.PlatformID, info.PlatformName)
			fmt.Fprintf(tw, "bootloader\t%s (%s)\n", info.BootloaderID, info.BootloaderName)
			fmt.Fprintf(tw, "bootloader-supported\t%s\n", boolWord(info.BootloaderSupported))
			fmt.Fprintf(tw, "hook-supported\t%s\n", boolWord(info.HookSupported))
			fmt.Fprintf(tw, "boot-dir\t%s\n", info.Layout.BootDir)
			fmt.Fprintf(tw, "esp-root\t%s\n", info.Layout.ESPRoot)
			fmt.Fprintf(tw, "efi-mirror-dir\t%s\n", info.Layout.EFIMirrorDir)
			fmt.Fprintf(tw, "snapshot-dir\t%s\n", info.Layout.SnapshotDir)
			fmt.Fprintf(tw, "root-modules-dir\t%s\n", info.Layout.RootModulesDir)
			fmt.Fprintf(tw, "grub-custom\t%s\n", info.Layout.GrubCustom)
			fmt.Fprintf(tw, "grub-cfg-output\t%s\n", info.Layout.GrubCfgOutput)
			fmt.Fprintf(tw, "pacman-hook-path\t%s\n", info.Layout.PacmanHookPath)
			for _, warning := range info.Warnings {
				fmt.Fprintf(tw, "warning\t%s\n", warning)
			}
			return tw.Flush()
		},
	}
}

func newHookCmd() *cobra.Command {
	hookCmd := &cobra.Command{
		Use:   "hook",
		Short: "Manage package-manager hook integration",
	}
	installCmd := &cobra.Command{
		Use:   "install [absolute-binary-path]",
		Short: "Install or refresh the platform package-manager hook",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) == 1 {
				path = args[0]
			}
			if err := tui.InstallPacmanHook(path); err != nil {
				return err
			}
			fmt.Printf("installed package-manager hook at %s\n", tui.PacmanHookPath)
			return nil
		},
	}
	hookCmd.AddCommand(installCmd)
	return hookCmd
}

func newBootloaderCmd() *cobra.Command {
	bootloaderCmd := &cobra.Command{
		Use:     "bootloader",
		Aliases: []string{"bl"},
		Short:   "Manage bootloader recovery entries",
	}
	bootloaderCmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List Bootrecov bootloader entries",
			RunE: func(cmd *cobra.Command, args []string) error {
				return printBootloaderEntries()
			},
		},
		&cobra.Command{
			Use:   "activate <snapshot-name>",
			Short: "Activate a snapshot for EFI + bootloader booting",
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
			Short: "Deactivate a snapshot and remove its EFI mirror and bootloader entry",
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
			Use:   "recovery <snapshot-name>",
			Short: "Print bootloader recovery commands for an activated snapshot",
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
	return bootloaderCmd
}

func newGrubCmd() *cobra.Command {
	grubCmd := &cobra.Command{
		Use:        "grub",
		Short:      "Compatibility alias for bootloader commands",
		Deprecated: "use bootrecov bootloader list",
	}
	grubCmd.AddCommand(&cobra.Command{
		Use:        "list",
		Short:      "List Bootrecov GRUB entries",
		Deprecated: "use bootrecov bootloader list",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printBootloaderEntries()
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
				fmt.Fprintln(tw, "NAME\tSNAPSHOT\tEFI\tBOOTLOADER\tBOOTABLE\tROOT-MODULES\tCREATED\tSIZE\tKERNEL")
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
			Short: "Activate a snapshot for EFI + bootloader booting",
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
			Short: "Deactivate a snapshot and remove its EFI mirror and bootloader entry",
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
			Short: "Delete a snapshot and its related EFI/bootloader artifacts",
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
			Short: "Print bootloader recovery commands for an activated snapshot",
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

func requireRiskAcknowledgement() error {
	if riskAccepted || riskAcceptedFromEnv() {
		return nil
	}
	if !stdinIsTerminal() {
		return fmt.Errorf("risk acknowledgement required; rerun with --yes-i-understand or %s=1", riskAcceptEnv)
	}
	fmt.Fprintln(os.Stderr, "bootrecov modifies boot-critical files and can make a system unbootable.")
	fmt.Fprintln(os.Stderr, "There is no warranty. You use this software entirely at your own risk.")
	fmt.Fprintf(os.Stderr, "Type %q to continue: ", riskAcceptPhrase)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return err
	}
	if strings.TrimSpace(line) != riskAcceptPhrase {
		return fmt.Errorf("risk acknowledgement was not accepted")
	}
	return nil
}

func riskAcceptedFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(riskAcceptEnv))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func printBootloaderEntries() error {
	entries, err := tui.ListGrubEntries()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("no bootloader entries found")
		return nil
	}
	tw := newTabWriter()
	fmt.Fprintln(tw, "ID\tNAME\tPATH")
	for _, entry := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", entry.ID, entry.Name, entry.BackupPath)
	}
	return tw.Flush()
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
