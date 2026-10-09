package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotDoesNotUseRunningKernelForUnknownImage(t *testing.T) {
	for _, fileResult := range []string{"missing", "unrecognized output", "unrecognized version token"} {
		t.Run(fileResult, func(t *testing.T) {
			boot, snap, efi, grub := setupDirs(t)
			setTestGlobals(t, boot, snap, efi, grub)
			bin := t.TempDir()
			switch fileResult {
			case "unrecognized output":
				writeExecutable(t, filepath.Join(bin, "file"), "#!/bin/sh\necho 'unrecognized image'\n")
			case "unrecognized version token":
				writeExecutable(t, filepath.Join(bin, "file"), "#!/bin/sh\necho 'unrecognized image, version 6.9.9-running'\n")
			}
			writeExecutable(t, filepath.Join(bin, "uname"), "#!/bin/sh\necho '6.9.9-running'\n")
			t.Setenv("PATH", bin)
			makeBootableBackup(t, boot, "")
			// The running kernel's modules belong to A; the copied unversioned
			// image could be B after an update before reboot.
			writeFile(t, filepath.Join(RootModulesDir, "6.9.9-running", "modules.dep"))

			created, err := CreateBootBackupNow()
			if err != nil {
				t.Fatal(err)
			}
			if created.KernelVersion != "unknown" || created.RootModulesKnown || created.HasArchivedModules {
				t.Fatalf("running kernel was misattributed to copied image: %#v", created)
			}
			if _, err := os.Stat(archivedModuleImagePath(created.SnapshotPath, "6.9.9-running")); !os.IsNotExist(err) {
				t.Fatalf("running kernel modules were archived for unknown image: %v", err)
			}
		})
	}
}

func TestLegacyModuleArchiveDoesNotProveUnversionedImageIdentity(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeBootableBackup(t, snap, "legacy")
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "legacy"), "6.9.9-running"))
	backups, err := DiscoverBackups()
	if err != nil || len(backups) != 1 {
		t.Fatalf("discover legacy snapshot: backups=%#v err=%v", backups, err)
	}
	if backups[0].KernelVersion != "unknown" || backups[0].RootModulesKnown || backups[0].HasArchivedModules {
		t.Fatalf("archive name was mistaken for image proof: %#v", backups[0])
	}
	if statusString(backups[0]) != "Unknown kernel" || IsBootReady(backups[0]) {
		t.Fatalf("unknown identity was reported as ready: %#v", backups[0])
	}
	if err := ActivateBackup("legacy"); !errors.Is(err, ErrBackupIncomplete) || !strings.Contains(err.Error(), "kernel version") {
		t.Fatalf("unknown kernel version activated without proof: %v", err)
	}
	if fileExists(filepath.Join(efi, "legacy")) || fileExists(grub) {
		t.Fatal("unknown recovery was published before rejection")
	}
}

func TestSnapshotUsesIdentifiedDiskKernelInsteadOfRunningKernel(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "file"), "#!/bin/sh\necho 'Linux kernel image, version 6.10.1-disk (builder)'\n")
	writeExecutable(t, filepath.Join(bin, "uname"), "#!/bin/sh\necho '6.9.9-running'\n")
	t.Setenv("PATH", bin)
	makeBootableBackup(t, boot, "")
	for _, version := range []string{"6.9.9-running", "6.10.1-disk"} {
		writeFile(t, filepath.Join(RootModulesDir, version, "modules.dep"))
	}
	created, err := CreateBootBackupNow()
	if err != nil {
		t.Fatal(err)
	}
	if created.KernelVersion != "6.10.1-disk" || !created.HasArchivedModules {
		t.Fatalf("disk kernel identity was not retained: %#v", created)
	}
	if _, err := os.Stat(archivedModuleImagePath(created.SnapshotPath, "6.10.1-disk")); err != nil {
		t.Fatalf("disk kernel modules were not archived: %v", err)
	}
	if _, err := os.Stat(archivedModuleImagePath(created.SnapshotPath, "6.9.9-running")); !os.IsNotExist(err) {
		t.Fatalf("running kernel modules were archived for disk image: %v", err)
	}
}

func TestSnapshotRejectsFilenameVersionContradictedByImage(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "file"), "#!/bin/sh\necho 'Linux kernel image, version 6.10.1-disk (builder)'\n")
	t.Setenv("PATH", bin)
	writeFile(t, filepath.Join(boot, "vmlinuz-6.9.9-old"))
	writeFile(t, filepath.Join(boot, "initrd.img-6.9.9-old"))
	writeFile(t, filepath.Join(RootModulesDir, "6.9.9-old", "modules.dep"))
	created, err := CreateBootBackupNow()
	if err != nil {
		t.Fatal(err)
	}
	if created.KernelVersion != "unknown" || created.HasArchivedModules {
		t.Fatalf("contradictory image was assigned filename's modules: %#v", created)
	}
	if err := ActivateBackup(created.Name); !errors.Is(err, ErrBackupIncomplete) {
		t.Fatalf("contradictory image was activated: %v", err)
	}
}

func TestReconcileRetainsLegacyEntryWithUnknownKernelIdentity(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeBootableBackup(t, snap, "legacy")
	makeBootableBackup(t, efi, "legacy")
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "legacy"), "6.9.9-running"))
	id := backupIDForName("legacy")
	visible := grubVisiblePath(filepath.Join(efi, "legacy"))
	entry := fmt.Sprintf("#!/bin/bash\ncat <<'EOF'\nmenuentry 'Bootrecov %s' --id %s {\n    linux %s/vmlinuz rw\n    initrd %s/initrd.img\n}\nEOF\n", filepath.Join(efi, "legacy"), id, visible, visible)
	if err := os.WriteFile(grub, []byte(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	backups, entries, err := SyncBackupsAndGrub()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(backups) != 1 || backups[0].InSync || backups[0].KernelVersion != "unknown" || IsBootReady(backups[0]) {
		t.Fatalf("ambiguous legacy recovery was misreported or removed: backups=%#v entries=%#v", backups, entries)
	}
	if _, err := os.Stat(filepath.Join(efi, "legacy", "vmlinuz")); err != nil {
		t.Fatalf("legacy mirror was removed: %v", err)
	}
	if err := AddGrubEntry(BootBackup{Name: "legacy"}); !errors.Is(err, ErrBackupIncomplete) {
		t.Fatalf("ambiguous recovery was re-published: %v", err)
	}
	if _, err := RecoveryCommands("legacy"); !errors.Is(err, ErrBackupIncomplete) {
		t.Fatalf("ambiguous recovery produced manual commands: %v", err)
	}
}

func TestUnknownLegacyEntryOutsideMirrorCannotBeVerified(t *testing.T) {
	for _, backend := range []string{"GRUB", "BLS"} {
		t.Run(backend, func(t *testing.T) {
			boot, snap, efi, grub := setupDirs(t)
			setTestGlobals(t, boot, snap, efi, grub)
			makeBootableBackup(t, snap, "legacy")
			makeBootableBackup(t, efi, "legacy")
			id := backupIDForName("legacy")
			if backend == "BLS" {
				activePlatformID = PlatformFedora
				writeFileWithContent(t, blsEntryPath(id), fmt.Sprintf("title Bootrecov %s\nlinux /defunct/vmlinuz\ninitrd /defunct/initrd.img\nid %s\n", filepath.Join(efi, "legacy"), id))
			} else {
				entry := fmt.Sprintf("#!/bin/bash\ncat <<'EOF'\nmenuentry 'Bootrecov %s' --id %s {\n    linux /defunct/vmlinuz rw\n    initrd /defunct/initrd.img\n}\nEOF\n", filepath.Join(efi, "legacy"), id)
				if err := os.WriteFile(grub, []byte(entry), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := SyncBackupsAndGrub(); !errors.Is(err, ErrBackupIncomplete) {
				t.Fatalf("wrong-path %s entry was treated as mirror recovery: %v", backend, err)
			}
			if _, err := os.Stat(filepath.Join(efi, "legacy", "vmlinuz")); err != nil {
				t.Fatalf("mirror changed after verification failure: %v", err)
			}
		})
	}
}
