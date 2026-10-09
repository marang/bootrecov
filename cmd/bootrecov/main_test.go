package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/tabwriter"

	"github.com/marang/bootrecov/internal/tui"
)

func TestHookBackupNowSkipsInsufficientSpaceErrors(t *testing.T) {
	oldCreate := createBootBackupNow
	createBootBackupNow = func() (tui.BootBackup, error) {
		return tui.BootBackup{}, fmt.Errorf("%w: test", tui.ErrInsufficientSnapshotSpace)
	}
	t.Cleanup(func() { createBootBackupNow = oldCreate })
	t.Setenv(riskAcceptEnv, "1")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"hook", "backup-now"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("hook backup-now should skip insufficient space errors, got %v", err)
	}
}

func TestHookBackupNowReturnsNonSpaceErrors(t *testing.T) {
	expected := errors.New("permission denied")
	oldCreate := createBootBackupNow
	createBootBackupNow = func() (tui.BootBackup, error) {
		return tui.BootBackup{}, expected
	}
	t.Cleanup(func() { createBootBackupNow = oldCreate })
	t.Setenv(riskAcceptEnv, "1")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"hook", "backup-now"})
	if err := cmd.Execute(); !errors.Is(err, expected) {
		t.Fatalf("hook backup-now should return non-space errors, got %v", err)
	}
}

func TestHookReconcileActiveRunsSync(t *testing.T) {
	called := false
	oldSync := syncBackupsAndGrub
	syncBackupsAndGrub = func() ([]tui.BootBackup, []tui.GrubEntry, error) {
		called = true
		return []tui.BootBackup{{Name: "active"}}, []tui.GrubEntry{{Name: "active"}}, nil
	}
	t.Cleanup(func() { syncBackupsAndGrub = oldSync })
	t.Setenv(riskAcceptEnv, "1")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"hook", "reconcile-active"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("hook reconcile-active failed: %v", err)
	}
	if !called {
		t.Fatal("expected hook reconcile-active to run active fallback sync")
	}
}

func TestHookReconcileActiveWarnsButDoesNotFailTransaction(t *testing.T) {
	expected := errors.New("efi mount unavailable")
	oldSync := syncBackupsAndGrub
	syncBackupsAndGrub = func() ([]tui.BootBackup, []tui.GrubEntry, error) {
		return nil, nil, expected
	}
	t.Cleanup(func() { syncBackupsAndGrub = oldSync })
	t.Setenv(riskAcceptEnv, "1")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"hook", "reconcile-active"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("post-transaction hook should warn without failing pacman, got %v", err)
	}
}

func TestHookUninstallRemovesHook(t *testing.T) {
	oldHookPath := tui.PacmanHookPath
	oldPostHookPath := tui.PacmanPostHookPath
	oldMkinitcpioInstall := tui.MkinitcpioInstallPath
	oldMkinitcpioHook := tui.MkinitcpioHookPath
	oldMkinitcpioConf := tui.MkinitcpioConfPath
	oldUpdateInitramfs := tui.UpdateInitramfs
	hookDir := t.TempDir()
	tui.PacmanHookPath = filepath.Join(hookDir, "bootrecov.hook")
	tui.PacmanPostHookPath = filepath.Join(hookDir, "bootrecov-post.hook")
	tui.MkinitcpioInstallPath = filepath.Join(hookDir, "initcpio", "install", "bootrecov")
	tui.MkinitcpioHookPath = filepath.Join(hookDir, "initcpio", "hooks", "bootrecov")
	tui.MkinitcpioConfPath = filepath.Join(hookDir, "mkinitcpio.conf")
	tui.UpdateInitramfs = false
	t.Cleanup(func() {
		tui.PacmanHookPath = oldHookPath
		tui.PacmanPostHookPath = oldPostHookPath
		tui.MkinitcpioInstallPath = oldMkinitcpioInstall
		tui.MkinitcpioHookPath = oldMkinitcpioHook
		tui.MkinitcpioConfPath = oldMkinitcpioConf
		tui.UpdateInitramfs = oldUpdateInitramfs
	})
	if err := os.WriteFile(tui.PacmanHookPath, []byte("hook"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tui.PacmanPostHookPath, []byte("hook"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(tui.MkinitcpioInstallPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(tui.MkinitcpioHookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tui.MkinitcpioInstallPath, []byte("hook"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tui.MkinitcpioHookPath, []byte("hook"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tui.MkinitcpioConfPath, []byte("HOOKS=(base filesystems bootrecov fsck)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(riskAcceptEnv, "1")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"hook", "uninstall"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("hook uninstall failed: %v", err)
	}
	if _, err := os.Stat(tui.PacmanHookPath); !os.IsNotExist(err) {
		t.Fatalf("expected pre hook to be removed, err=%v", err)
	}
	if _, err := os.Stat(tui.PacmanPostHookPath); !os.IsNotExist(err) {
		t.Fatalf("expected post hook to be removed, err=%v", err)
	}
	if _, err := os.Stat(tui.MkinitcpioInstallPath); !os.IsNotExist(err) {
		t.Fatalf("expected mkinitcpio install hook to be removed, err=%v", err)
	}
	if _, err := os.Stat(tui.MkinitcpioHookPath); !os.IsNotExist(err) {
		t.Fatalf("expected mkinitcpio runtime hook to be removed, err=%v", err)
	}
}

func TestPackagePreRemoveCleansMkinitcpioConfig(t *testing.T) {
	base := t.TempDir()
	preHook := filepath.Join(base, "hooks", "pre.hook")
	postHook := filepath.Join(base, "hooks", "post.hook")
	installHook := filepath.Join(base, "initcpio", "install", "bootrecov")
	runtimeHook := filepath.Join(base, "initcpio", "hooks", "bootrecov")
	conf := filepath.Join(base, "mkinitcpio.conf")
	for _, path := range []string{preHook, postHook, installHook, runtimeHook} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("hook"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(conf, []byte("MODULES=()\nHOOKS=(base filesystems bootrecov keyboard fsck)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(base, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mkinitcpioLog := filepath.Join(base, "mkinitcpio.log")
	mkinitcpio := filepath.Join(binDir, "mkinitcpio")
	if err := os.WriteFile(mkinitcpio, []byte(fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >%s\n", shellQuote(mkinitcpioLog))), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "-c", ". ../../bootrecov.install; pre_remove")
	cmd.Env = append(os.Environ(),
		"BOOTRECOV_PACMAN_HOOK_PATH="+preHook,
		"BOOTRECOV_PACMAN_POST_HOOK_PATH="+postHook,
		"BOOTRECOV_MKINITCPIO_INSTALL_HOOK="+installHook,
		"BOOTRECOV_MKINITCPIO_RUNTIME_HOOK="+runtimeHook,
		"BOOTRECOV_MKINITCPIO_CONF="+conf,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pre_remove failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	for _, path := range []string{preHook, postHook, installHook, runtimeHook} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("expected %s to be removed, stat err=%v", path, statErr)
		}
	}
	confData, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(confData), "bootrecov") {
		t.Fatalf("expected bootrecov hook to be removed from mkinitcpio config:\n%s", string(confData))
	}
	logData, err := os.ReadFile(mkinitcpioLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(logData)) != "-P" {
		t.Fatalf("expected mkinitcpio -P to run, log=%q", string(logData))
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func TestRiskConfirmationAcceptedUsesDefaultNo(t *testing.T) {
	for _, input := range []string{"", "\n", "n", "N", "no", "anything else"} {
		if riskConfirmationAccepted(input) {
			t.Fatalf("expected %q to reject risk acknowledgement", input)
		}
	}
}

func TestRiskConfirmationAcceptedAllowsYes(t *testing.T) {
	for _, input := range []string{"y", "Y", "yes", "YES", " yes \n"} {
		if !riskConfirmationAccepted(input) {
			t.Fatalf("expected %q to accept risk acknowledgement", input)
		}
	}
}

func TestRenderRiskAcknowledgementPromptLooksLikePanel(t *testing.T) {
	prompt := renderRiskAcknowledgementPrompt()
	for _, want := range []string{"Bootrecov risk acknowledgement", "Continue? [y/N]", "╭", "╰"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, strings.Join([]string{"I", "UNDERSTAND"}, " ")) {
		t.Fatalf("prompt should not use the old phrase: %s", prompt)
	}
}

func renderDoctorPlatformRows(t *testing.T, info tui.RuntimeEnvironment) string {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	printPlatformDoctorRows(tw, info)
	if err := tw.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestDoctorPlatformRowsShowOnlyArchHookBackend(t *testing.T) {
	out := renderDoctorPlatformRows(t, tui.RuntimeEnvironment{
		PlatformID: tui.PlatformArch,
		Layout: tui.SystemLayout{
			PacmanHookPath:        "/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook",
			PacmanPostHookPath:    "/etc/pacman.d/hooks/96-bootrecov-post-transaction.hook",
			MkinitcpioInstallHook: "/usr/lib/initcpio/install/bootrecov",
			MkinitcpioRuntimeHook: "/usr/lib/initcpio/hooks/bootrecov",
			MkinitcpioConfig:      "/etc/mkinitcpio.conf",
			MkinitcpioBin:         "/usr/bin/mkinitcpio",
			DracutBin:             "dracut",
		},
	})
	for _, want := range []string{
		"package-hook-backend",
		"supported",
		"pacman",
		"initramfs-backend",
		"mkinitcpio",
		"mkinitcpio-bin",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected Arch doctor rows to contain %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"dracut-bin", "dnf5-actions-path", "bls-entries-dir"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("Arch doctor rows should not contain %q:\n%s", unwanted, out)
		}
	}
}

func TestDoctorPlatformRowsShowOnlyFedoraHookBackend(t *testing.T) {
	out := renderDoctorPlatformRows(t, tui.RuntimeEnvironment{
		PlatformID: tui.PlatformFedora,
		Layout: tui.SystemLayout{
			MkinitcpioBin:       "mkinitcpio",
			BLSEntriesDir:       "/boot/loader/entries",
			DNF5ActionsPath:     "/etc/dnf/libdnf5-plugins/actions.d/95-bootrecov.actions",
			DNF4PreActionsPath:  "/etc/dnf/plugins/pre-transaction-actions.d/95-bootrecov.action",
			DNF4PostActionsPath: "/etc/dnf/plugins/post-transaction-actions.d/95-bootrecov.action",
			DracutModuleDir:     "/usr/lib/dracut/modules.d/95bootrecov",
			DracutBin:           "/usr/sbin/dracut",
		},
	})
	for _, want := range []string{
		"package-hook-backend",
		"dnf actions",
		"initramfs-backend",
		"dracut",
		"dracut-bin",
		"bls-entries-dir",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected Fedora doctor rows to contain %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"mkinitcpio-bin", "pacman-pre-hook"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("Fedora doctor rows should not contain %q:\n%s", unwanted, out)
		}
	}
}

func TestDoctorPlatformRowsShowMissingFedoraDracut(t *testing.T) {
	out := renderDoctorPlatformRows(t, tui.RuntimeEnvironment{
		PlatformID: tui.PlatformFedora,
		Layout: tui.SystemLayout{
			BLSEntriesDir:   "/boot/loader/entries",
			DracutModuleDir: "/usr/lib/dracut/modules.d/95bootrecov",
			DracutBin:       "bootrecov-missing-dracut",
		},
	})
	if !strings.Contains(out, "dracut-bin") || !strings.Contains(out, "missing") {
		t.Fatalf("expected Fedora doctor rows to report missing dracut:\n%s", out)
	}
}

func TestDoctorPlatformRowsShowUnsupportedDebianHookBackend(t *testing.T) {
	out := renderDoctorPlatformRows(t, tui.RuntimeEnvironment{
		PlatformID: tui.PlatformDebian,
		Layout: tui.SystemLayout{
			DracutBin: "dracut",
		},
	})
	for _, want := range []string{
		"apt/dpkg",
		"not implemented",
		"initramfs-tools",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected Debian doctor rows to contain %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"dracut-bin", "mkinitcpio-bin", "dnf5-actions-path"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("Debian doctor rows should not contain %q:\n%s", unwanted, out)
		}
	}
}

func TestHookReconcileActiveReportsCompletedCleanupWarning(t *testing.T) {
	oldSync := syncBackupsAndGrub
	syncBackupsAndGrub = func() ([]tui.BootBackup, []tui.GrubEntry, error) {
		return []tui.BootBackup{{Name: "active"}}, []tui.GrubEntry{}, &tui.ReconcileCleanupWarning{Cause: errors.New("package database unavailable")}
	}
	t.Cleanup(func() { syncBackupsAndGrub = oldSync })
	t.Setenv(riskAcceptEnv, "1")
	var output bytes.Buffer
	cmd := newRootCmd()
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"hook", "reconcile-active"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("completed reconcile cleanup warning should not fail package transaction: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "reconciled 1 backups and 0 bootloader entries") || !strings.Contains(got, "restored module cleanup incomplete after package transaction: package database unavailable") || strings.Contains(got, "reconcile failed") {
		t.Fatalf("hook misreported completed reconcile: %q", got)
	}
}

func TestManualReconcileReturnsPartialFailureWithCompletedCount(t *testing.T) {
	want := errors.New("injected mirror sync failure")
	partial := &tui.ReconcilePartialError{
		Issues:        []tui.ReconcileIssue{{Snapshot: "active", Operation: "sync mirror", Cause: want, WasActive: true, EntryRetained: true, MirrorPresent: true}},
		Completed:     []tui.ReconcileAction{{Snapshot: "obsolete", Operation: "remove inactive mirror"}},
		StateVerified: true,
	}
	oldSync := syncBackupsAndGrub
	syncBackupsAndGrub = func() ([]tui.BootBackup, []tui.GrubEntry, error) {
		return []tui.BootBackup{{Name: "active"}, {Name: "obsolete"}}, []tui.GrubEntry{{Name: "active"}}, partial
	}
	t.Cleanup(func() { syncBackupsAndGrub = oldSync })
	t.Setenv(riskAcceptEnv, "1")
	var output bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"reconcile"})
	err := cmd.Execute()
	if !errors.Is(err, want) {
		t.Fatalf("manual reconcile did not fail for partial result: %v", err)
	}
	if !strings.Contains(output.String(), "processed 2 backups and 1 bootloader entries") || !strings.Contains(output.String(), "completed: obsolete: remove inactive mirror") {
		t.Fatalf("successful work was not reported separately: %q", output.String())
	}
}

func TestHookReconcileActiveWarnsWhenRecoveryEntryIsLost(t *testing.T) {
	want := errors.New("injected module restore failure")
	partial := &tui.ReconcilePartialError{Issues: []tui.ReconcileIssue{{Snapshot: "active", Operation: "restore modules", Cause: want, WasActive: true, MirrorPresent: true}}, StateVerified: true}
	oldSync := syncBackupsAndGrub
	syncBackupsAndGrub = func() ([]tui.BootBackup, []tui.GrubEntry, error) {
		return []tui.BootBackup{{Name: "active"}}, []tui.GrubEntry{}, partial
	}
	t.Cleanup(func() { syncBackupsAndGrub = oldSync })
	t.Setenv(riskAcceptEnv, "1")
	var output bytes.Buffer
	cmd := newRootCmd()
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"hook", "reconcile-active"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("post-transaction hook must warn without failing the completed transaction: %v", err)
	}
	got := output.String()
	for _, phrase := range []string{"active", "restore modules", "injected module restore failure", "recovery entry lost"} {
		if !strings.Contains(got, phrase) {
			t.Fatalf("post-transaction warning omitted %q: %q", phrase, got)
		}
	}
}

func TestHookReconcileActiveSeparatesPartialAndCleanupWarnings(t *testing.T) {
	partial := &tui.ReconcilePartialError{Issues: []tui.ReconcileIssue{{Snapshot: "active", Operation: "sync mirror", Cause: errors.New("sync unavailable"), WasActive: true, EntryRetained: true, MirrorPresent: true}}, StateVerified: true}
	cleanup := &tui.ReconcileCleanupWarning{Cause: errors.New("package database unavailable")}
	oldSync := syncBackupsAndGrub
	syncBackupsAndGrub = func() ([]tui.BootBackup, []tui.GrubEntry, error) {
		return []tui.BootBackup{{Name: "active"}}, []tui.GrubEntry{{Name: "active"}}, errors.Join(partial, cleanup)
	}
	t.Cleanup(func() { syncBackupsAndGrub = oldSync })
	t.Setenv(riskAcceptEnv, "1")
	var output bytes.Buffer
	cmd := newRootCmd()
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"hook", "reconcile-active"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("post-transaction hook failed: %v", err)
	}
	got := output.String()
	for _, phrase := range []string{"snapshot active sync mirror failed: sync unavailable", "recovery readiness unverified", "restored module cleanup incomplete after package transaction: package database unavailable"} {
		if !strings.Contains(got, phrase) {
			t.Fatalf("combined warnings omitted %q: %q", phrase, got)
		}
	}
}

func TestHookReconcileActiveDoesNotClaimLostEntryForInactiveMirror(t *testing.T) {
	partial := &tui.ReconcilePartialError{Issues: []tui.ReconcileIssue{{Snapshot: "inactive", Operation: "remove inactive mirror", Cause: errors.New("removal denied"), MirrorPresent: true}}, StateVerified: true}
	oldSync := syncBackupsAndGrub
	syncBackupsAndGrub = func() ([]tui.BootBackup, []tui.GrubEntry, error) {
		return []tui.BootBackup{{Name: "inactive"}}, []tui.GrubEntry{}, partial
	}
	t.Cleanup(func() { syncBackupsAndGrub = oldSync })
	t.Setenv(riskAcceptEnv, "1")
	var output bytes.Buffer
	cmd := newRootCmd()
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"hook", "reconcile-active"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "snapshot inactive remove inactive mirror failed: removal denied") || strings.Contains(got, "recovery entry lost") {
		t.Fatalf("inactive mirror failure was misreported: %q", got)
	}
}

func TestManualReconcileDoesNotClaimFinalEntryCountAfterListingFailure(t *testing.T) {
	want := errors.New("entry listing unavailable")
	partial := &tui.ReconcilePartialError{Completed: []tui.ReconcileAction{{Snapshot: "obsolete", Operation: "remove inactive mirror"}}, FinalizationError: want}
	oldSync := syncBackupsAndGrub
	syncBackupsAndGrub = func() ([]tui.BootBackup, []tui.GrubEntry, error) {
		return []tui.BootBackup{{Name: "obsolete"}}, nil, partial
	}
	t.Cleanup(func() { syncBackupsAndGrub = oldSync })
	t.Setenv(riskAcceptEnv, "1")
	var output bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"reconcile"})
	if err := cmd.Execute(); !errors.Is(err, want) {
		t.Fatalf("finalization failure was not returned: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "final bootloader entry state unverified") || strings.Contains(got, "0 bootloader entries") || !strings.Contains(got, "completed: obsolete: remove inactive mirror") {
		t.Fatalf("manual reconcile overstated final state: %q", got)
	}
}
