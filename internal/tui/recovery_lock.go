package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Shared across installations that may point at the same module tree.
var RecoveryLockDir = "/run/lock/bootrecov"

// Serialize snapshot, entry, and module-tree mutations across Bootrecov
// processes. In particular, activation must not race module cleanup while
// DKMS is removing a build for the same kernel.
func withRecoveryOperation(fn func() error) error {
	lockDir := filepath.Clean(RecoveryLockDir)
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return fmt.Errorf("prepare recovery operation lock: %w", err)
	}
	lockPath := filepath.Join(lockDir, ".bootrecov-operations.lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open recovery operation lock: %w", err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock recovery operation: %w", err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return fn()
}

func withRecoveryBackupOperation(fn func() (BootBackup, error)) (BootBackup, error) {
	var backup BootBackup
	err := withRecoveryOperation(func() error {
		var inner error
		backup, inner = fn()
		return inner
	})
	return backup, err
}

func withRecoverySyncOperation(fn func() ([]BootBackup, []GrubEntry, error)) ([]BootBackup, []GrubEntry, error) {
	var backups []BootBackup
	var entries []GrubEntry
	err := withRecoveryOperation(func() error {
		var inner error
		backups, entries, inner = fn()
		return inner
	})
	return backups, entries, err
}
