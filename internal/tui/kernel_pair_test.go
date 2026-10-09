package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotCreationRejectsMismatchedKernelPair(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFile(t, filepath.Join(boot, "vmlinuz-6.10.1"))
	writeFile(t, filepath.Join(boot, "initrd.img-6.9.9"))
	_, err := CreateBootBackupNow()
	if !errors.Is(err, ErrBackupIncomplete) || !strings.Contains(err.Error(), "6.10.1") || !strings.Contains(err.Error(), "6.9.9") {
		t.Fatalf("mismatched kernel pair accepted or versions omitted: %v", err)
	}
	entries, err := os.ReadDir(snap)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid snapshot published: entries=%#v err=%v", entries, err)
	}
}

func TestActivationRejectsMismatchedKernelPair(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	name := "mismatch"
	writeFile(t, filepath.Join(snap, name, "vmlinuz-6.10.1"))
	writeFile(t, filepath.Join(snap, name, "initrd.img-6.9.9"))
	err := ActivateBackup(name)
	if !errors.Is(err, ErrBackupIncomplete) || !strings.Contains(err.Error(), "6.10.1") || !strings.Contains(err.Error(), "6.9.9") {
		t.Fatalf("mismatched kernel pair activated or versions omitted: %v", err)
	}
	if fileExists(filepath.Join(efi, name)) || fileExists(grub) {
		t.Fatal("invalid pair created mirror or GRUB entry")
	}
}

func TestSnapshotCreationAcceptsProvenKernelPairs(t *testing.T) {
	for _, tc := range []struct {
		name, kernel, initramfs string
		extra                   []string
	}{
		{"Arch", "vmlinuz-linux", "initramfs-linux.img", nil},
		{"Arch LTS fallback", "vmlinuz-linux-lts", "initramfs-linux-lts-fallback.img", nil},
		{"Debian and Ubuntu", "vmlinuz-6.8.0-31-generic", "initrd.img-6.8.0-31-generic", nil},
		{"Fedora", "vmlinuz-6.9.8-200.fc40.x86_64", "initramfs-6.9.8-200.fc40.x86_64.img", nil},
		{"unversioned exact pair", "vmlinuz", "initrd.img", nil},
		{"matching among multiple kernels", "vmlinuz-7.0.0-arch1-1", "initrd.img-7.0.0-arch1-1", []string{"vmlinuz-6.6.7-arch1-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			boot, snap, efi, grub := setupDirs(t)
			setTestGlobals(t, boot, snap, efi, grub)
			for _, name := range append([]string{tc.kernel, tc.initramfs}, tc.extra...) {
				writeFile(t, filepath.Join(boot, name))
			}
			created, err := CreateBootBackupNow()
			if err != nil {
				t.Fatal(err)
			}
			if created.KernelImage != tc.kernel || created.InitramfsImage != tc.initramfs {
				t.Fatalf("selected %q/%q, want %q/%q", created.KernelImage, created.InitramfsImage, tc.kernel, tc.initramfs)
			}
		})
	}
}

func TestSnapshotCreationRejectsUnpairedArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name, kernel, initramfs string
	}{
		{"missing initramfs", "vmlinuz-6.10.1", ""},
		{"unrelated versioned initramfs", "vmlinuz", "initramfs-6.10.1.img"},
		{"different Arch flavors", "vmlinuz-linux-lts", "initramfs-linux-zen.img"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			boot, snap, efi, grub := setupDirs(t)
			setTestGlobals(t, boot, snap, efi, grub)
			writeFile(t, filepath.Join(boot, tc.kernel))
			if tc.initramfs != "" {
				writeFile(t, filepath.Join(boot, tc.initramfs))
			}
			if _, err := CreateBootBackupNow(); !errors.Is(err, ErrBackupIncomplete) {
				t.Fatalf("unpaired artifacts accepted: %v", err)
			}
		})
	}
}

func TestReconcilePreservesExistingEntryWhenSnapshotChangesPair(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeFixtureKernelVersionDiscoverable(t)
	makeBootableBackup(t, snap, "recovery")
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(snap, "recovery")
	writeFile(t, filepath.Join(source, "vmlinuz-linux"))
	writeFile(t, filepath.Join(source, "initramfs-linux.img"))
	writeFileWithContent(t, filepath.Join(source, "vmlinuz"), "")
	backups, entries, err := SyncBackupsAndGrub()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(backups) != 1 || backups[0].InSync {
		t.Fatalf("changed pair should leave old entry intact but out of sync: backups=%#v entries=%#v", backups, entries)
	}
	contents, err := os.ReadFile(filepath.Join(efi, "recovery", "vmlinuz"))
	if err != nil || string(contents) != "data" {
		t.Fatalf("old entry kernel overwritten: content=%q err=%v", contents, err)
	}
}

func TestActivationRejectsExistingEntryWithDifferentPair(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeFixtureKernelVersionDiscoverable(t)
	makeBootableBackup(t, snap, "recovery")
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(snap, "recovery")
	writeFile(t, filepath.Join(source, "vmlinuz-linux"))
	writeFile(t, filepath.Join(source, "initramfs-linux.img"))
	err := ActivateBackup("recovery")
	if !errors.Is(err, ErrBackupIncomplete) || !strings.Contains(err.Error(), "deactivate") {
		t.Fatalf("changed pair silently activated: %v", err)
	}
	if fileExists(filepath.Join(efi, "recovery", "vmlinuz-linux")) {
		t.Fatal("changed pair copied before rejection")
	}
	entries, err := ListGrubEntries()
	if err != nil || len(entries) != 1 {
		t.Fatalf("old entry lost: entries=%#v err=%v", entries, err)
	}
}

func TestUnknownExistingKernelDirectiveBlocksMirrorMutation(t *testing.T) {
	for _, action := range []struct {
		name string
		run  func() error
	}{
		{"activation", func() error { return ActivateBackup("recovery") }},
		{"reconcile", func() error { _, _, err := SyncBackupsAndGrub(); return err }},
	} {
		t.Run(action.name, func(t *testing.T) {
			boot, snap, efi, grub := setupDirs(t)
			setTestGlobals(t, boot, snap, efi, grub)
			makeFixtureKernelVersionDiscoverable(t)
			makeBootableBackup(t, snap, "recovery")
			if err := ActivateBackup("recovery"); err != nil {
				t.Fatal(err)
			}
			entry, err := os.ReadFile(grub)
			if err != nil {
				t.Fatal(err)
			}
			changed := strings.Replace(string(entry), "    linux ", "    linuxefi ", 1)
			if changed == string(entry) {
				t.Fatal("test entry did not contain expected kernel directive")
			}
			if err := os.WriteFile(grub, []byte(changed), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(snap, "recovery", "vmlinuz-linux"))
			writeFile(t, filepath.Join(snap, "recovery", "initramfs-linux.img"))
			if err := action.run(); !errors.Is(err, ErrBackupIncomplete) {
				t.Fatalf("unknown entry syntax did not stop %s: %v", action.name, err)
			}
			if fileExists(filepath.Join(efi, "recovery", "vmlinuz-linux")) {
				t.Fatal("mirror was mutated despite unknown entry syntax")
			}
		})
	}
}

func TestBLSActivationRejectsExistingEntryWithDifferentPair(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeFixtureKernelVersionDiscoverable(t)
	activePlatformID = PlatformFedora
	if err := os.MkdirAll(BLSEntriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeBootableBackup(t, snap, "recovery")
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(blsEntryPath(backupIDForName("recovery"))); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(snap, "recovery", "vmlinuz-linux"))
	writeFile(t, filepath.Join(snap, "recovery", "initramfs-linux.img"))
	if err := ActivateBackup("recovery"); !errors.Is(err, ErrBackupIncomplete) {
		t.Fatalf("changed BLS pair silently activated: %v", err)
	}
	if fileExists(filepath.Join(efi, "recovery", "vmlinuz-linux")) {
		t.Fatal("changed pair copied before rejection")
	}
}

func TestBLSReconcilePreservesExistingEntryWhenSnapshotChangesPair(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeFixtureKernelVersionDiscoverable(t)
	activePlatformID = PlatformFedora
	if err := os.MkdirAll(BLSEntriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeBootableBackup(t, snap, "recovery")
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(snap, "recovery")
	writeFile(t, filepath.Join(source, "vmlinuz-linux"))
	writeFile(t, filepath.Join(source, "initramfs-linux.img"))
	writeFileWithContent(t, filepath.Join(source, "vmlinuz"), "")
	backups, entries, err := SyncBackupsAndGrub()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(backups) != 1 || backups[0].InSync {
		t.Fatalf("changed BLS pair should leave old entry out of sync: backups=%#v entries=%#v", backups, entries)
	}
	contents, err := os.ReadFile(filepath.Join(efi, "recovery", "vmlinuz"))
	if err != nil || string(contents) != "data" {
		t.Fatalf("old BLS kernel overwritten: content=%q err=%v", contents, err)
	}
}

func TestReconcilePreservesOldModulesWhenNewPairLacksModules(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	oldVersion := "6.6.7-arch1-1"
	newVersion := "7.0.0-arch1-1"
	makeVersionedBootableBackup(t, snap, "recovery", oldVersion)
	writeFile(t, filepath.Join(RootModulesDir, oldVersion, "modules.dep"))
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(snap, "recovery")
	if err := os.Remove(filepath.Join(source, "vmlinuz-"+oldVersion)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(source, "vmlinuz-"+newVersion))
	writeFile(t, filepath.Join(source, "initrd.img-"+newVersion))
	backups, entries, err := SyncBackupsAndGrub()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(backups) != 1 || backups[0].InSync {
		t.Fatalf("old entry lost because new kernel lacks modules: backups=%#v entries=%#v", backups, entries)
	}
	if !fileExists(filepath.Join(efi, "recovery", "vmlinuz-"+oldVersion)) {
		t.Fatal("old entry kernel removed from mirror")
	}
}

func TestReconcileDropsOldEntryWhenOnlyNewKernelHasModules(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	oldVersion := "6.6.7-arch1-1"
	newVersion := "7.0.0-arch1-1"
	makeVersionedBootableBackup(t, snap, "recovery", oldVersion)
	writeFile(t, filepath.Join(RootModulesDir, oldVersion, "modules.dep"))
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(RootModulesDir, oldVersion)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(RootModulesDir, newVersion, "modules.dep"))
	source := filepath.Join(snap, "recovery")
	if err := os.Remove(filepath.Join(source, "vmlinuz-"+oldVersion)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(source, "vmlinuz-"+newVersion))
	writeFile(t, filepath.Join(source, "initrd.img-"+newVersion))
	_, entries, err := SyncBackupsAndGrub()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("old entry retained without its modules: %#v", entries)
	}
}

func TestReconcileRestoresOldArchivedModulesAfterPairChange(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	oldVersion := "6.6.7-arch1-1"
	newVersion := "7.0.0-arch1-1"
	makeVersionedBootableBackup(t, snap, "recovery", oldVersion)
	writeFile(t, filepath.Join(RootModulesDir, oldVersion, "modules.dep"))
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "recovery"), oldVersion))
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(RootModulesDir, oldVersion)); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(snap, "recovery")
	if err := os.Remove(filepath.Join(source, "vmlinuz-"+oldVersion)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(source, "vmlinuz-"+newVersion))
	writeFile(t, filepath.Join(source, "initrd.img-"+newVersion))
	_, entries, err := SyncBackupsAndGrub()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("recoverable old entry removed: %#v", entries)
	}
	if !fileExists(filepath.Join(RootModulesDir, oldVersion, "modules.dep")) {
		t.Fatal("old archived modules were not restored")
	}
}

func TestReconcileKeepsOldEntryWhenArchivedModuleRestoreFails(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	oldVersion := "6.6.7-arch1-1"
	newVersion := "7.0.0-arch1-1"
	makeVersionedBootableBackup(t, snap, "recovery", oldVersion)
	writeFile(t, filepath.Join(RootModulesDir, oldVersion, "modules.dep"))
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "recovery"), oldVersion))
	if err := ActivateBackup("recovery"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(RootModulesDir, oldVersion)); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(snap, "recovery")
	if err := os.Remove(filepath.Join(source, "vmlinuz-"+oldVersion)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(source, "vmlinuz-"+newVersion))
	writeFile(t, filepath.Join(source, "initrd.img-"+newVersion))
	restoreModuleTreeFunc = func(_, _ string) error { return errors.New("simulated restore failure") }
	if _, _, err := SyncBackupsAndGrub(); !errors.Is(err, ErrRootModuleRestoreFailed) {
		t.Fatalf("restore failure hidden: %v", err)
	}
	entries, err := ListGrubEntries()
	if err != nil || len(entries) != 1 {
		t.Fatalf("old entry removed despite restore failure: entries=%#v err=%v", entries, err)
	}
}
