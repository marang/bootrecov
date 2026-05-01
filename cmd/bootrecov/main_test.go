package main

import (
	"errors"
	"fmt"
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
