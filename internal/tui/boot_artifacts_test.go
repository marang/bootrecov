package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverBackupsRejectsInvalidBootArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, snapshot, mirror string)
	}{
		{"empty snapshot kernel", func(t *testing.T, snapshot, mirror string) {
			writeFileWithContent(t, filepath.Join(snapshot, "vmlinuz"), "")
			writeFileWithContent(t, filepath.Join(mirror, "vmlinuz"), "")
		}},
		{"empty mirror initramfs", func(t *testing.T, snapshot, mirror string) {
			writeFileWithContent(t, filepath.Join(mirror, "initrd.img"), "")
		}},
		{"mirror kernel directory", func(t *testing.T, snapshot, mirror string) {
			path := filepath.Join(mirror, "vmlinuz")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"mirror kernel symlink", func(t *testing.T, snapshot, mirror string) {
			path := filepath.Join(mirror, "vmlinuz")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(snapshot, "vmlinuz"), path); err != nil {
				t.Fatal(err)
			}
		}},
		{"mirror kernel truncated", func(t *testing.T, snapshot, mirror string) {
			writeFileWithContent(t, filepath.Join(snapshot, "vmlinuz"), "original kernel")
			writeFileWithContent(t, filepath.Join(mirror, "vmlinuz"), "original")
		}},
		{"same-size mirror initramfs mismatch", func(t *testing.T, snapshot, mirror string) {
			writeFileWithContent(t, filepath.Join(mirror, "initrd.img"), "fake")
		}},
		{"mirror microcode mismatch", func(t *testing.T, snapshot, mirror string) {
			writeFileWithContent(t, filepath.Join(snapshot, "intel-ucode.img"), "microcode")
			writeFileWithContent(t, filepath.Join(mirror, "intel-ucode.img"), "different")
		}},
		{"dangling snapshot microcode symlink", func(t *testing.T, snapshot, mirror string) {
			if err := os.Symlink("missing-ucode.img", filepath.Join(snapshot, "intel-ucode.img")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			boot, snap, efi, grub := setupDirs(t)
			setTestGlobals(t, boot, snap, efi, grub)
			makeBootableBackup(t, snap, "recovery")
			makeBootableBackup(t, efi, "recovery")
			tc.mutate(t, filepath.Join(snap, "recovery"), filepath.Join(efi, "recovery"))
			backups, err := DiscoverBackups()
			if err != nil {
				t.Fatal(err)
			}
			if len(backups) != 1 {
				t.Fatalf("got %d backups", len(backups))
			}
			if backups[0].InSync || IsBootReady(backups[0]) {
				t.Fatalf("invalid artifact reported ready: %#v", backups[0])
			}
			if statusString(backups[0]) != "Incomplete" {
				t.Fatalf("invalid mirror reported OK: %#v", backups[0])
			}
			if err := AddGrubEntry(backups[0]); !errors.Is(err, ErrBackupNotActivated) {
				t.Fatalf("invalid mirror accepted for GRUB: %v", err)
			}
		})
	}
}

func TestReconcileKeepsExistingMirrorWhenSnapshotArtifactIsInvalid(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeFixtureKernelVersionDiscoverable(t)
	makeBootableBackup(t, snap, "recovery")
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	writeFileWithContent(t, filepath.Join(snap, "recovery", "vmlinuz"), "")
	if _, _, err := SyncBackupsAndGrub(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(efi, "recovery", "vmlinuz"))
	if err != nil || string(contents) != "data" {
		t.Fatalf("good mirror damaged by invalid snapshot: content=%q err=%v", contents, err)
	}
	entries, err := ListGrubEntries()
	if err != nil || len(entries) != 1 {
		t.Fatalf("existing recovery entry was removed: entries=%#v err=%v", entries, err)
	}
}

func TestReconcileDropsEntryWhenSnapshotAndMirrorAreInvalid(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeFixtureKernelVersionDiscoverable(t)
	makeBootableBackup(t, snap, "recovery")
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	writeFileWithContent(t, filepath.Join(snap, "recovery", "vmlinuz"), "")
	writeFileWithContent(t, filepath.Join(efi, "recovery", "vmlinuz"), "")
	if _, _, err := SyncBackupsAndGrub(); err != nil {
		t.Fatal(err)
	}
	entries, err := ListGrubEntries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid recovery entry retained: entries=%#v err=%v", entries, err)
	}
}

func TestReconcileDoesNotPreserveEntryForUnrelatedMirrorPair(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeFixtureKernelVersionDiscoverable(t)
	makeBootableBackup(t, snap, "recovery")
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	mirror := filepath.Join(efi, "recovery")
	writeFile(t, filepath.Join(mirror, "vmlinuz-linux"))
	writeFile(t, filepath.Join(mirror, "initramfs-linux.img"))
	writeFileWithContent(t, filepath.Join(snap, "recovery", "vmlinuz"), "")
	writeFileWithContent(t, filepath.Join(mirror, "vmlinuz"), "")
	if _, _, err := SyncBackupsAndGrub(); err != nil {
		t.Fatal(err)
	}
	entries, err := ListGrubEntries()
	if err != nil || len(entries) != 0 {
		t.Fatalf("entry with invalid referenced kernel retained: entries=%#v err=%v", entries, err)
	}
}

func TestRecoveryCommandsRejectDamagedMirror(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeFixtureKernelVersionDiscoverable(t)
	makeBootableBackup(t, snap, "recovery")
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	writeFileWithContent(t, filepath.Join(efi, "recovery", "vmlinuz"), "")
	if commands, err := RecoveryCommands("recovery"); !errors.Is(err, ErrBackupIncomplete) || commands != "" {
		t.Fatalf("damaged mirror produced recovery commands: commands=%q err=%v", commands, err)
	}
}

func TestSnapshotStatusRejectsInvalidMicrocodeWithoutMirror(t *testing.T) {
	for _, name := range []string{"empty", "directory", "symlink", "dangling symlink"} {
		t.Run(name, func(t *testing.T) {
			boot, snap, efi, grub := setupDirs(t)
			setTestGlobals(t, boot, snap, efi, grub)
			makeBootableBackup(t, snap, "recovery")
			path := filepath.Join(snap, "recovery", "intel-ucode.img")
			switch name {
			case "empty":
				writeFileWithContent(t, path, "")
			case "directory":
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("vmlinuz", path); err != nil {
					t.Fatal(err)
				}
			case "dangling symlink":
				if err := os.Symlink("missing-ucode.img", path); err != nil {
					t.Fatal(err)
				}
			}
			backups, err := DiscoverBackups()
			if err != nil {
				t.Fatal(err)
			}
			if len(backups) != 1 || statusString(backups[0]) != "Incomplete" {
				t.Fatalf("invalid microcode reported OK: %#v", backups)
			}
		})
	}
}

func TestActivateBackupRepairsExistingDamagedMirror(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeFixtureKernelVersionDiscoverable(t)
	makeBootableBackup(t, snap, "recovery")
	makeBootableBackup(t, efi, "recovery")
	writeFileWithContent(t, filepath.Join(efi, "recovery", "vmlinuz"), "fake")
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	backups, err := DiscoverBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || !IsBootReady(backups[0]) {
		t.Fatalf("expected repaired mirror to be ready: %#v", backups)
	}
	if !fileExists(grub) {
		t.Fatal("missing GRUB entry")
	}
}

func TestActivateBackupRepairsMirrorDespiteLowEstimatedFreeSpace(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeFixtureKernelVersionDiscoverable(t)
	makeBootableBackup(t, snap, "recovery")
	makeBootableBackup(t, efi, "recovery")
	writeFileWithContent(t, filepath.Join(efi, "recovery", "vmlinuz"), "fake")
	setFreeBytes(t, 0)
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatalf("existing mirror repair blocked by full-copy estimate: %v", err)
	}
}

func TestActivateBackupRejectsInvalidSnapshotArtifact(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeBootableBackup(t, snap, "recovery")
	writeFileWithContent(t, filepath.Join(snap, "recovery", "vmlinuz"), "")
	if err := ActivateBackup("recovery"); !errors.Is(err, ErrBackupIncomplete) {
		t.Fatalf("invalid snapshot accepted: %v", err)
	}
	if fileExists(filepath.Join(efi, "recovery")) || fileExists(grub) {
		t.Fatal("invalid snapshot caused mirror or GRUB mutation")
	}
}

func TestActivateBackupReportsMirrorMismatchAfterSync(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeBootableBackup(t, snap, "recovery")
	makeBootableBackup(t, efi, "recovery")
	writeFileWithContent(t, filepath.Join(efi, "recovery", "vmlinuz"), "fake")
	rclone := filepath.Join(t.TempDir(), "rclone")
	writeExecutable(t, rclone, "#!/bin/sh\nexit 0\n")
	RcloneBin = rclone
	RequireRclone = true
	if err := ActivateBackup("recovery"); !errors.Is(err, ErrBackupIncomplete) {
		t.Fatalf("expected concrete mismatch error, got %v", err)
	}
	if fileExists(grub) {
		t.Fatal("GRUB entry created for damaged mirror")
	}
}

func TestMirrorRcloneSyncUsesChecksum(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	rclone := filepath.Join(t.TempDir(), "rclone")
	writeExecutable(t, rclone, "#!/bin/sh\ncase \" $* \" in *\" --checksum \"*) exit 0;; *) exit 23;; esac\n")
	RcloneBin = rclone
	if err := runRcloneSyncMode(snap, efi, nil, nil, true); err != nil {
		t.Fatalf("mirror sync did not use checksum: %v", err)
	}
	if err := runRcloneSync(snap, efi, nil, nil); !errors.Is(err, ErrSyncFailed) {
		t.Fatalf("normal sync unexpectedly used checksum: %v", err)
	}
}

func TestActivateBackupRepairsSameSizeSameMtimeMirrorWithRclone(t *testing.T) {
	rclone, err := exec.LookPath("rclone")
	if err != nil {
		t.Skip("rclone is not installed")
	}
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeFixtureKernelVersionDiscoverable(t)
	makeBootableBackup(t, snap, "recovery")
	makeBootableBackup(t, efi, "recovery")
	source := filepath.Join(snap, "recovery", "vmlinuz")
	mirror := filepath.Join(efi, "recovery", "vmlinuz")
	writeFileWithContent(t, mirror, "fake")
	stamp := time.Unix(1700000000, 0)
	for _, path := range []string{source, mirror} {
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	RcloneBin = rclone
	RequireRclone = true
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	backups, err := DiscoverBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || !IsBootReady(backups[0]) {
		t.Fatalf("same-size corruption was not repaired: %#v", backups)
	}
}
