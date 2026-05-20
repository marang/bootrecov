package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
