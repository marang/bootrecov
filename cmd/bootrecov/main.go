package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/marang/bootrecov/internal/tui"
)

const (
	riskAcceptEnv     = "BOOTRECOV_ACCEPT_RISK"
	riskAcceptFlagMsg = "acknowledge that bootrecov has no warranty and is used at your own risk"
)

var (
	errRiskAcknowledgementRequired = errors.New("risk acknowledgement required")
	errRiskAcknowledgementRejected = errors.New("risk acknowledgement rejected")
	riskAccepted                   bool
	createBootBackupNow            = tui.CreateBootBackupNow
	syncBackupsAndGrub             = tui.SyncBackupsAndGrub
	riskPromptStyle                = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("214")).Padding(0, 1)
	riskTitleStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	riskMutedStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	doctorOKStyle                  = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	doctorWarnStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	doctorErrorStyle               = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	doctorNAStyle                  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

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
			backups, entries, err := syncBackupsAndGrub()
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
			fmt.Fprintln(tw, "GROUP\tKEY\tSTATUS\tVALUE")
			printDoctorRow(tw, "core", "platform", statusOK("detected"), "%s (%s)", info.PlatformID, info.PlatformName)
			printDoctorRow(tw, "core", "bootloader", doctorBoolStatus(info.BootloaderSupported, "supported", "unsupported"), "%s (%s)", info.BootloaderID, info.BootloaderName)
			printDoctorRow(tw, "core", "package-hooks", doctorBoolStatus(info.HookSupported, "supported", "not implemented"), "%s", boolWord(info.HookSupported))
			printDoctorPathRow(tw, "paths", "boot-dir", info.Layout.BootDir, true)
			printDoctorPathRow(tw, "paths", "esp-root", info.Layout.ESPRoot, true)
			printDoctorPathRow(tw, "paths", "active-mirror-dir", info.Layout.EFIMirrorDir, false)
			printDoctorPathRow(tw, "paths", "snapshot-dir", info.Layout.SnapshotDir, false)
			printDoctorPathRow(tw, "paths", "root-modules-dir", info.Layout.RootModulesDir, true)
			printDoctorPathRow(tw, "grub", "custom-file", info.Layout.GrubCustom, false)
			printDoctorPathRow(tw, "grub", "config-output", info.Layout.GrubCfgOutput, false)
			printPlatformDoctorRows(tw, info)
			for _, warning := range info.Warnings {
				printDoctorRow(tw, "warnings", "warning", statusWarn("check"), "%s", warning)
			}
			return tw.Flush()
		},
	}
}

func printPlatformDoctorRows(tw *tabwriter.Writer, info tui.RuntimeEnvironment) {
	switch info.PlatformID {
	case tui.PlatformArch:
		printDoctorRow(tw, "arch", "package-hook-backend", statusOK("supported"), "pacman")
		printDoctorPathRow(tw, "arch", "pacman-pre-hook", info.Layout.PacmanHookPath, false)
		printDoctorPathRow(tw, "arch", "pacman-post-hook", info.Layout.PacmanPostHookPath, false)
		printDoctorRow(tw, "arch", "initramfs-backend", statusOK("supported"), "mkinitcpio")
		printDoctorBinRow(tw, "arch", "mkinitcpio-bin", info.Layout.MkinitcpioBin)
		printDoctorPathRow(tw, "arch", "mkinitcpio-config", info.Layout.MkinitcpioConfig, true)
		printDoctorPathRow(tw, "arch", "mkinitcpio-install-hook", info.Layout.MkinitcpioInstallHook, false)
		printDoctorPathRow(tw, "arch", "mkinitcpio-runtime-hook", info.Layout.MkinitcpioRuntimeHook, false)
	case tui.PlatformFedora:
		printDoctorRow(tw, "fedora", "package-hook-backend", statusOK("supported"), "dnf actions")
		printDoctorPathRow(tw, "fedora", "dnf5-actions", info.Layout.DNF5ActionsPath, false)
		printDoctorPathRow(tw, "fedora", "dnf4-pre-actions", info.Layout.DNF4PreActionsPath, false)
		printDoctorPathRow(tw, "fedora", "dnf4-post-actions", info.Layout.DNF4PostActionsPath, false)
		printDoctorRow(tw, "fedora", "initramfs-backend", statusOK("supported"), "dracut")
		printDoctorBinRow(tw, "fedora", "dracut-bin", info.Layout.DracutBin)
		printDoctorPathRow(tw, "fedora", "dracut-module-dir", info.Layout.DracutModuleDir, false)
		printDoctorPathRow(tw, "fedora", "bls-entries-dir", info.Layout.BLSEntriesDir, true)
	case tui.PlatformUbuntu, tui.PlatformDebian:
		printDoctorRow(tw, info.PlatformID, "package-hook-backend", statusWarn("not implemented"), "apt/dpkg")
		printDoctorRow(tw, info.PlatformID, "initramfs-backend", statusWarn("not implemented"), "initramfs-tools")
	default:
		printDoctorRow(tw, "platform", "package-hook-backend", statusNA("n/a"), "unknown")
		printDoctorRow(tw, "platform", "initramfs-backend", statusNA("n/a"), "unknown")
	}
}

type doctorStatus struct {
	label string
	style lipgloss.Style
}

func statusOK(label string) doctorStatus {
	return doctorStatus{label: label, style: doctorOKStyle}
}

func statusWarn(label string) doctorStatus {
	return doctorStatus{label: label, style: doctorWarnStyle}
}

func statusError(label string) doctorStatus {
	return doctorStatus{label: label, style: doctorErrorStyle}
}

func statusNA(label string) doctorStatus {
	return doctorStatus{label: label, style: doctorNAStyle}
}

func doctorBoolStatus(ok bool, okLabel string, badLabel string) doctorStatus {
	if ok {
		return statusOK(okLabel)
	}
	return statusWarn(badLabel)
}

func printDoctorRow(tw *tabwriter.Writer, group string, key string, status doctorStatus, format string, args ...any) {
	fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", group, key, renderDoctorStatus(status), fmt.Sprintf(format, args...))
}

func printDoctorPathRow(tw *tabwriter.Writer, group string, key string, path string, required bool) {
	status := pathStatus(path, required)
	printDoctorRow(tw, group, key, status, "%s", valueOrDash(path))
}

func printDoctorBinRow(tw *tabwriter.Writer, group string, key string, bin string) {
	status := binStatus(bin)
	value := valueOrDash(bin)
	if resolved, err := exec.LookPath(bin); err == nil && resolved != "" {
		value = resolved
	}
	printDoctorRow(tw, group, key, status, "%s", value)
}

func pathStatus(path string, required bool) doctorStatus {
	if strings.TrimSpace(path) == "" {
		if required {
			return statusError("missing")
		}
		return statusNA("n/a")
	}
	if _, err := os.Stat(path); err == nil {
		return statusOK("present")
	}
	parent := filepath.Dir(path)
	if parent != "." && parent != path {
		if _, err := os.Stat(parent); err == nil {
			if required {
				return statusError("missing")
			}
			return statusWarn("not installed")
		}
	}
	if required {
		return statusError("missing")
	}
	return statusWarn("not available")
}

func binStatus(bin string) doctorStatus {
	if strings.TrimSpace(bin) == "" {
		return statusError("missing")
	}
	if _, err := exec.LookPath(bin); err == nil {
		return statusOK("available")
	}
	return statusError("missing")
}

func renderDoctorStatus(status doctorStatus) string {
	label := status.label
	if label == "" {
		label = "unknown"
	}
	if os.Getenv("NO_COLOR") != "" {
		return label
	}
	return status.style.Render(label)
}

func valueOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
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
			if err := tui.InstallPlatformHooks(path); err != nil {
				return err
			}
			fmt.Printf("installed platform package-manager and initramfs hooks\n")
			return nil
		},
	}
	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the package-manager hook",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			removed, err := tui.UninstallPlatformHooks()
			if err != nil {
				return err
			}
			if removed {
				fmt.Printf("removed platform package-manager and initramfs hooks\n")
				return nil
			}
			fmt.Printf("platform package-manager and initramfs hooks are not installed\n")
			return nil
		},
	}
	backupNowCmd := &cobra.Command{
		Use:    "backup-now",
		Hidden: true,
		Short:  "Create a pre-transaction snapshot from a package-manager hook",
		RunE: func(cmd *cobra.Command, args []string) error {
			created, err := createBootBackupNow()
			if err != nil {
				if tui.IsInsufficientSpaceError(err) {
					fmt.Fprintf(os.Stderr, "bootrecov warning: skipping pre-transaction backup: %v\n", err)
					return nil
				}
				return err
			}
			fmt.Println(created.Name)
			return nil
		},
	}
	reconcileActiveCmd := &cobra.Command{
		Use:    "reconcile-active",
		Hidden: true,
		Short:  "Refresh active recovery entries from a package-manager hook",
		RunE: func(cmd *cobra.Command, args []string) error {
			backups, entries, err := syncBackupsAndGrub()
			var warning *tui.ReconcileCleanupWarning
			if err != nil && !errors.As(err, &warning) {
				fmt.Fprintf(cmd.ErrOrStderr(), "bootrecov warning: active fallback reconcile failed after package transaction: %v\n", err)
				return nil
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "bootrecov: reconciled %d backups and %d bootloader entries after package transaction\n", len(backups), len(entries))
			if warning != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "bootrecov warning: restored module cleanup incomplete after package transaction: %v\n", warning.Cause)
			}
			return nil
		},
	}
	hookCmd.AddCommand(installCmd, uninstallCmd, backupNowCmd, reconcileActiveCmd)
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
				fmt.Fprintln(tw, "NAME\tSNAPSHOT\tEFI\tBOOTLOADER\tBOOTABLE\tRESTORABLE\tROOT-MODULES\tCREATED\tSIZE\tKERNEL")
				for _, b := range backups {
					fmt.Fprintf(
						tw,
						"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
						b.Name,
						boolWord(b.HasSnapshot),
						boolWord(b.HasEFI),
						boolWord(b.GrubEntryExists),
						boolWord(isBootable(b)),
						boolWord(isRestorable(b)),
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
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	return nil
}

func requireRiskAcknowledgement() error {
	if riskAccepted || riskAcceptedFromEnv() {
		return nil
	}
	if !stdinIsTerminal() {
		return fmt.Errorf("%w; rerun with --yes-i-understand or %s=1", errRiskAcknowledgementRequired, riskAcceptEnv)
	}
	fmt.Fprint(os.Stderr, renderRiskAcknowledgementPrompt())
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return err
	}
	if !riskConfirmationAccepted(line) {
		return errRiskAcknowledgementRejected
	}
	return nil
}

func renderRiskAcknowledgementPrompt() string {
	body := strings.Join([]string{
		riskTitleStyle.Render("Bootrecov risk acknowledgement"),
		"Bootrecov modifies boot-critical files and can make a system unbootable.",
		"There is no warranty. You use this software entirely at your own risk.",
	}, "\n")
	return riskPromptStyle.Render(body) + "\n" + riskMutedStyle.Render("Continue? [y/N] ")
}

func riskConfirmationAccepted(input string) bool {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "y", "yes":
		return true
	default:
		return false
	}
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

func isRestorable(b tui.BootBackup) bool {
	return tui.IsRestoreReady(b)
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
