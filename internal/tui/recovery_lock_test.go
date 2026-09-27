package tui

import (
	"testing"
	"time"
)

func TestRecoveryOperationsSerializeModuleCleanupAndActivation(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- withRecoveryOperation(func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first operation did not acquire the lock")
	}

	acquired := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- withRecoveryOperation(func() error {
			close(acquired)
			return nil
		})
	}()
	select {
	case <-acquired:
		t.Fatal("second operation entered while the first held the lock")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second operation did not acquire the released lock")
	}
}
