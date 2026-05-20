package main

import (
	"errors"
	"fmt"
	"os"
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
