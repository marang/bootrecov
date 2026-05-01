package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func setupDirs(t *testing.T) (string, string, string, string) {
	t.Helper()
	base := t.TempDir()
	boot := filepath.Join(base, "boot")
	snap := filepath.Join(base, "snapshots")
	efi := filepath.Join(base, "efi")
	grub := filepath.Join(base, "grub")
	for _, p := range []string{boot, snap, efi} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return boot, snap, efi, grub
}

func setTestGlobals(t *testing.T, boot, snap, efi, grub string) {
	t.Helper()
	oldBoot, oldSnap, oldEFI, oldGrub, oldGrubCfg, oldMkconfig, oldAutoGrub, oldModules, oldHookPath, oldRclone, oldRequire, oldMksquashfs, oldRequireMksquashfs, oldRequireEFIMount, oldCreateImage, oldStatfs, oldMountInfo, oldOSReleasePath, oldGrubDefaultPath, oldPlatformOverride, oldBootloaderOverride, oldActivePlatformID, oldActivePlatformName, oldActiveHookSupported, oldActiveBootloaderID, oldActiveBootloaderName, oldActiveWarnings :=
		BootDir, SnapshotDir, EfiDir, GrubCustom, GrubCfgOutput, GrubMkconfig, AutoUpdateGrub, RootModulesDir, PacmanHookPath, RcloneBin, RequireRclone, MksquashfsBin, RequireMksquashfs, RequireEFIMount, createModuleImageFunc, statfsFunc, mountInfoPath, OSReleasePath, GrubDefaultPath, PlatformOverride, BootloaderOverride, activePlatformID, activePlatformName, activeHookSupported, activeBootloaderID, activeBootloaderName, activeWarnings
	BootDir, SnapshotDir, EfiDir, GrubCustom = boot, snap, efi, grub
	GrubCfgOutput = filepath.Join(filepath.Dir(grub), "grub.cfg")
	GrubMkconfig = ""
	AutoUpdateGrub = false
	RootModulesDir = filepath.Join(filepath.Dir(grub), "modules")
	PacmanHookPath = filepath.Join(filepath.Dir(grub), "bootrecov.hook")
	RcloneBin = ""
	RequireRclone = false
	MksquashfsBin = ""
	RequireMksquashfs = false
	RequireEFIMount = false
	createModuleImageFunc = fakeCreateModuleImage
	statfsFunc = syscall.Statfs
	OSReleasePath = filepath.Join(filepath.Dir(grub), "os-release")
	GrubDefaultPath = filepath.Join(filepath.Dir(grub), "default-grub")
	PlatformOverride = ""
	BootloaderOverride = ""
	activePlatformID = PlatformArch
	activePlatformName = "Arch Linux"
	activeHookSupported = true
	activeBootloaderID = BootloaderGRUB
	activeBootloaderName = "GRUB"
	activeWarnings = nil
	t.Cleanup(func() {
		BootDir, SnapshotDir, EfiDir, GrubCustom, GrubCfgOutput, GrubMkconfig, AutoUpdateGrub, RootModulesDir, PacmanHookPath, RcloneBin, RequireRclone, MksquashfsBin, RequireMksquashfs, RequireEFIMount, createModuleImageFunc, statfsFunc, mountInfoPath, OSReleasePath, GrubDefaultPath, PlatformOverride, BootloaderOverride, activePlatformID, activePlatformName, activeHookSupported, activeBootloaderID, activeBootloaderName, activeWarnings =
			oldBoot, oldSnap, oldEFI, oldGrub, oldGrubCfg, oldMkconfig, oldAutoGrub, oldModules, oldHookPath, oldRclone, oldRequire, oldMksquashfs, oldRequireMksquashfs, oldRequireEFIMount, oldCreateImage, oldStatfs, oldMountInfo, oldOSReleasePath, oldGrubDefaultPath, oldPlatformOverride, oldBootloaderOverride, oldActivePlatformID, oldActivePlatformName, oldActiveHookSupported, oldActiveBootloaderID, oldActiveBootloaderName, oldActiveWarnings
	})
}

func fakeCreateModuleImage(src, dst string) error {
	if !dirExists(src) {
		return fmt.Errorf("%w: source module tree does not exist: %s", ErrSourceDirectoryMissing, src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, []byte("squashfs"), 0o644)
}

func TestDetectPlatformFromOSRelease(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{name: "arch", data: "ID=arch\nPRETTY_NAME=\"Arch Linux\"\n", want: PlatformArch},
		{name: "ubuntu", data: "ID=ubuntu\nID_LIKE=debian\nPRETTY_NAME=\"Ubuntu 24.04\"\n", want: PlatformUbuntu},
		{name: "debian-like", data: "ID=pop\nID_LIKE=\"ubuntu debian\"\n", want: PlatformUbuntu},
		{name: "unknown", data: "ID=void\n", want: "void"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := detectPlatformFromOSRelease(parseOSRelease([]byte(tc.data)))
			if got != tc.want {
				t.Fatalf("platform=%q want %q", got, tc.want)
			}
		})
	}
}

func TestConfigureDetectedEnvironmentHonorsOverrides(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	PlatformOverride = PlatformUbuntu
	BootloaderOverride = BootloaderGRUB

	info := ConfigureDetectedEnvironment()
	if info.PlatformID != PlatformUbuntu {
		t.Fatalf("expected ubuntu platform, got %#v", info)
	}
	if info.BootloaderID != BootloaderGRUB || !info.BootloaderSupported {
		t.Fatalf("expected supported grub bootloader, got %#v", info)
	}
	if info.HookSupported {
		t.Fatalf("ubuntu hooks should not be implemented in first adapter cut: %#v", info)
	}
}

func TestConfigureDetectedEnvironmentWarningsAreIdempotent(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	PlatformOverride = PlatformUbuntu
	BootloaderOverride = BootloaderSystemdBoot

	first := ConfigureDetectedEnvironment()
	second := ConfigureDetectedEnvironment()

	if len(first.Warnings) != 2 {
		t.Fatalf("first warnings=%#v", first.Warnings)
	}
	if len(second.Warnings) != 2 {
		t.Fatalf("second warnings should not duplicate stale warnings: %#v", second.Warnings)
	}
}

func TestApplyEnvironmentOverridesFromEnv(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	espRoot := filepath.Join(filepath.Dir(efi), "esp")
	t.Setenv("BOOTRECOV_PLATFORM", "ubuntu")
	t.Setenv("BOOTRECOV_BOOTLOADER", "systemdboot")
	t.Setenv("BOOTRECOV_BOOT_DIR", filepath.Join(filepath.Dir(boot), "custom-boot"))
	t.Setenv("BOOTRECOV_ESP_DIR", espRoot)
	t.Setenv("BOOTRECOV_BACKUP_PROFILE", "minimal")

	ApplyEnvironmentOverridesFromEnv()

	if PlatformOverride != PlatformUbuntu {
		t.Fatalf("platform override=%q", PlatformOverride)
	}
	if BootloaderOverride != BootloaderSystemdBoot {
		t.Fatalf("bootloader override=%q", BootloaderOverride)
	}
	if BootDir != filepath.Join(filepath.Dir(boot), "custom-boot") {
		t.Fatalf("boot dir override=%q", BootDir)
	}
	if EfiDir != filepath.Join(espRoot, "bootrecov-snapshots") {
		t.Fatalf("efi mirror override=%q", EfiDir)
	}
	if BackupProfile != "minimal" {
		t.Fatalf("backup profile=%q", BackupProfile)
	}
}

func TestConfigureDetectedEnvironmentDetectsSystemdBootUnsupported(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	if err := os.MkdirAll(filepath.Join(filepath.Dir(efi), "loader", "entries"), 0o755); err != nil {
		t.Fatal(err)
	}

	info := ConfigureDetectedEnvironment()
	if info.BootloaderID != BootloaderSystemdBoot {
		t.Fatalf("expected systemd-boot detection, got %#v", info)
	}
	if info.BootloaderSupported {
		t.Fatalf("systemd-boot should be detected but unsupported in first adapter cut: %#v", info)
	}
}

func TestConfigureDetectedEnvironmentPrefersSystemdBootOverWeakGRUBSignal(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	if err := os.MkdirAll(filepath.Join(filepath.Dir(efi), "loader", "entries"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, GrubDefaultPath)

	info := ConfigureDetectedEnvironment()
	if info.BootloaderID != BootloaderSystemdBoot {
		t.Fatalf("expected systemd-boot to win over weak GRUB signal, got %#v", info)
	}
}

func TestConfigureDetectedEnvironmentRejectsAmbiguousBootloaderSignals(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	if err := os.MkdirAll(filepath.Join(filepath.Dir(efi), "loader", "entries"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, GrubCfgOutput)

	info := ConfigureDetectedEnvironment()
	if info.BootloaderID != BootloaderUnknown || info.BootloaderSupported {
		t.Fatalf("expected ambiguous bootloader to be unsupported unknown, got %#v", info)
	}
}

func TestDetectBootDirFromMountInfoArtifacts(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	detectedBoot := filepath.Join(filepath.Dir(boot), "custom-boot")
	writeFile(t, filepath.Join(detectedBoot, "vmlinuz-linux"))
	writeFile(t, filepath.Join(detectedBoot, "initramfs-linux.img"))
	BootDir = filepath.Join(filepath.Dir(boot), "missing-boot")
	mountInfoPath = filepath.Join(t.TempDir(), "mountinfo")
	content := fmt.Sprintf("35 24 8:2 / %s rw,relatime - ext4 /dev/sda1 rw\n", detectedBoot)
	if err := os.WriteFile(mountInfoPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := detectBootDir(); got != detectedBoot {
		t.Fatalf("detectBootDir=%q want %q", got, detectedBoot)
	}
}

func TestDetectESPRootFromVFATMountInfo(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	espRoot := filepath.Join(filepath.Dir(boot), "my-efi")
	if err := os.MkdirAll(filepath.Join(espRoot, "EFI"), 0o755); err != nil {
		t.Fatal(err)
	}
	mountInfoPath = filepath.Join(t.TempDir(), "mountinfo")
	content := fmt.Sprintf("36 24 8:3 / %s rw,relatime - vfat /dev/sda2 rw\n", espRoot)
	if err := os.WriteFile(mountInfoPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := detectESPRoot(); got != espRoot {
		t.Fatalf("detectESPRoot=%q want %q", got, espRoot)
	}
}

func TestDetectESPRootRejectsUnmarkedFATMount(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	usbRoot := filepath.Join(filepath.Dir(boot), "usb-stick")
	if err := os.MkdirAll(usbRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	mountInfoPath = filepath.Join(t.TempDir(), "mountinfo")
	content := fmt.Sprintf("36 24 8:3 / %s rw,relatime - vfat /dev/sdb1 rw\n", usbRoot)
	if err := os.WriteFile(mountInfoPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := detectESPRoot(); got != "" {
		t.Fatalf("unmarked FAT mount should not be detected as ESP, got %q", got)
	}
}

func TestBootTreeExcludesHandleESPAtBootRoot(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	BootDir = filepath.Join(filepath.Dir(boot), "esp-as-boot")
	EfiDir = filepath.Join(BootDir, "bootrecov-snapshots")

	patterns := bootTreeRcloneExcludePatterns()
	if !containsString(patterns, "bootrecov-snapshots/**") {
		t.Fatalf("expected EFI mirror exclude for ESP-at-/boot layout, got %#v", patterns)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func setFreeBytes(t *testing.T, freeBytes int64) {
	t.Helper()
	old := statfsFunc
	statfsFunc = func(_ string, st *syscall.Statfs_t) error {
		st.Bavail = uint64(freeBytes)
		st.Bsize = 1
		return nil
	}
	t.Cleanup(func() { statfsFunc = old })
}

func containsStringWithPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func makeBootableBackup(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "vmlinuz"))
	writeFile(t, filepath.Join(dir, "initrd.img"))
}

func makeVersionedBootableBackup(t *testing.T, root, name, version string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "vmlinuz-"+version))
	writeFile(t, filepath.Join(dir, "initrd.img-"+version))
}

func TestDiscoverBackupsDeduplicatesByName(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	makeBootableBackup(t, snap, "same")
	makeBootableBackup(t, efi, "same")

	backups, err := DiscoverBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 deduplicated backup, got %d", len(backups))
	}
	if backups[0].Name != "same" {
		t.Fatalf("unexpected backup name: %#v", backups[0])
	}
	if !backups[0].HasSnapshot || !backups[0].HasEFI || !backups[0].InSync {
		t.Fatalf("backup should be in-sync: %#v", backups[0])
	}
}

func TestCheckRuntimeDependenciesReportsMissingTools(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	RequireRclone = true
	RcloneBin = "definitely-missing-rclone"
	AutoUpdateGrub = true
	GrubMkconfig = "definitely-missing-grub-mkconfig"
	RequireMksquashfs = true
	MksquashfsBin = "definitely-missing-mksquashfs"

	err := CheckRuntimeDependencies()
	if err == nil {
		t.Fatal("expected missing dependency error")
	}
	if !errors.Is(err, ErrRuntimeDependenciesMissing) {
		t.Fatalf("expected missing dependency error, got: %v", err)
	}
	var depsErr *RuntimeDependenciesError
	if !errors.As(err, &depsErr) {
		t.Fatalf("expected RuntimeDependenciesError, got: %T", err)
	}
	for _, want := range []string{"definitely-missing-rclone", "definitely-missing-grub-mkconfig", "definitely-missing-mksquashfs"} {
		if !containsStringWithPrefix(depsErr.Missing, want) {
			t.Fatalf("expected missing dependency %q in %#v", want, depsErr.Missing)
		}
	}
}

func TestNewModelFailsEarlyWhenDependenciesMissing(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	RequireRclone = true
	RcloneBin = "definitely-missing-rclone"

	_, err := NewModel()
	if !errors.Is(err, ErrRuntimeDependenciesMissing) {
		t.Fatalf("expected startup dependency error, got %v", err)
	}
}

func TestSyncBackupsAndGrubRepairsMissingMirrorAndRemovesStale(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	makeBootableBackup(t, efi, "one")
	stalePath := filepath.Join(efi, "stale")
	staleID := backupID(stalePath)
	staleEntry := fmt.Sprintf("#!/bin/bash\ncat <<'EOF'\nmenuentry 'Bootrecov %s' --id %s {\n}\nEOF\n", stalePath, staleID)
	if err := os.WriteFile(grub, []byte(staleEntry), 0o755); err != nil {
		t.Fatal(err)
	}

	backups, entries, err := SyncBackupsAndGrub()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one backup, got %d", len(backups))
	}
	if backups[0].HasSnapshot || backups[0].HasEFI || backups[0].InSync {
		t.Fatalf("expected orphan EFI-only backup to be cleaned: %#v", backups[0])
	}
	if _, err := os.Stat(filepath.Join(snap, "one")); !os.IsNotExist(err) {
		t.Fatalf("snapshot should not be auto-created from EFI-only backup, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(efi, "one")); !os.IsNotExist(err) {
		t.Fatalf("EFI orphan should be removed during reconcile, err=%v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("stale grub entries should be removed, got: %#v", entries)
	}
}

func TestSyncBackupsAndGrubRemovesInactiveEFIMirror(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	makeBootableBackup(t, snap, "inactive")
	makeBootableBackup(t, efi, "inactive")

	backups, entries, err := SyncBackupsAndGrub()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no active grub entries, got %#v", entries)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one backup, got %d", len(backups))
	}
	if backups[0].HasEFI {
		t.Fatalf("expected inactive EFI mirror to be removed: %#v", backups[0])
	}
	if _, err := os.Stat(filepath.Join(efi, "inactive")); !os.IsNotExist(err) {
		t.Fatalf("inactive EFI mirror should be deleted during reconcile, err=%v", err)
	}
}

func TestSyncBackupsAndGrubPreservesActiveGrubEntryWhenRefreshFails(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	makeBootableBackup(t, snap, "active")
	makeBootableBackup(t, efi, "active")

	entryID := backupID(filepath.Join(efi, "active"))
	entry := fmt.Sprintf("#!/bin/bash\ncat <<'EOF'\nmenuentry 'Bootrecov %s' --id %s {\n}\nEOF\n", filepath.Join(efi, "active"), entryID)
	if err := os.WriteFile(grub, []byte(entry), 0o755); err != nil {
		t.Fatal(err)
	}

	RcloneBin = "definitely-missing-rclone-binary"
	RequireRclone = true

	backups, entries, err := SyncBackupsAndGrub()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one backup, got %d", len(backups))
	}
	if backups[0].HasEFI != true {
		t.Fatalf("expected existing EFI mirror to remain present: %#v", backups[0])
	}
	if backups[0].InSync {
		t.Fatalf("expected backup to be marked out of sync after refresh failure: %#v", backups[0])
	}
	if len(entries) != 1 || entries[0].ID != entryID {
		t.Fatalf("expected active grub entry to be preserved, got %#v", entries)
	}
}

func TestSyncBackupsAndGrubRequiresEFIMountBeforeMutation(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	RequireEFIMount = true
	mountInfoPath = filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(mountInfoPath, []byte("24 1 8:1 / / rw,relatime - ext4 /dev/root rw\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeBootableBackup(t, snap, "active")
	entryID := backupID(filepath.Join(efi, "active"))
	entry := fmt.Sprintf("#!/bin/bash\ncat <<'EOF'\nmenuentry 'Bootrecov %s' --id %s {\n}\nEOF\n", filepath.Join(efi, "active"), entryID)
	if err := os.WriteFile(grub, []byte(entry), 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, err := SyncBackupsAndGrub()
	if !errors.Is(err, ErrEFIMountUnavailable) {
		t.Fatalf("expected EFI mount error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(efi, "active")); !os.IsNotExist(statErr) {
		t.Fatalf("reconcile should not create EFI mirror when mount is unavailable, err=%v", statErr)
	}
}

func TestAddGrubEntryRequiresSyncedPair(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	makeBootableBackup(t, efi, "only-efi")
	err := AddGrubEntry(BootBackup{Name: "only-efi", Path: filepath.Join(efi, "only-efi")})
	if !errors.Is(err, ErrBackupNotActivated) {
		t.Fatalf("expected activation error, got: %v", err)
	}
}

func TestAddRemoveGrubEntryForSyncedPair(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	makeBootableBackup(t, snap, "pair")
	makeBootableBackup(t, efi, "pair")

	if err := AddGrubEntry(BootBackup{Name: "pair"}); err != nil {
		t.Fatalf("AddGrubEntry failed: %v", err)
	}
	entries, err := ListGrubEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 grub entry, got %d", len(entries))
	}
	if err := RemoveGrubEntry(entries[0].ID); err != nil {
		t.Fatalf("RemoveGrubEntry failed: %v", err)
	}
	entries, err = ListGrubEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries after remove, got %#v", entries)
	}
}

func TestAddGrubEntryRejectsStaleEFIMirrorMissingBootArtifacts(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	makeBootableBackup(t, snap, "stale")
	if err := os.MkdirAll(filepath.Join(efi, "stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(efi, "stale", "vmlinuz"))

	err := AddGrubEntry(BootBackup{Name: "stale"})
	if !errors.Is(err, ErrBackupNotActivated) {
		t.Fatalf("expected stale EFI mirror rejection, got %v", err)
	}
}

func TestBootloaderOperationsRejectUnsupportedSystemdBoot(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	activeBootloaderID = BootloaderSystemdBoot
	activeBootloaderName = "systemd-boot"

	err := ActivateBackup("anything")
	if !errors.Is(err, ErrUnsupportedBootloader) {
		t.Fatalf("expected unsupported bootloader error, got %v", err)
	}
}

func TestCreateBootBackupNowSkipsRecursiveEfiBackupCopy(t *testing.T) {
	boot, snap, _, grub := setupDirs(t)
	efi := filepath.Join(boot, "efi", "bootrecov-snapshots")
	setTestGlobals(t, boot, snap, efi, grub)

	if err := os.MkdirAll(filepath.Join(boot, "efi", "bootrecov-snapshots", "old"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(boot, "vmlinuz"))
	writeFile(t, filepath.Join(boot, "initrd.img"))
	writeFile(t, filepath.Join(boot, "efi", "bootrecov-snapshots", "old", "should-not-copy"))
	writeFile(t, filepath.Join(boot, "efi", "EFI", "BOOT", "BOOTX64.EFI"))

	created, err := CreateBootBackupNow()
	if err != nil {
		t.Fatalf("CreateBootBackupNow failed: %v", err)
	}
	if !created.HasKernel || !created.HasInitramfs || !created.HasSnapshot || created.HasEFI || !created.InSync {
		t.Fatalf("created snapshot should be complete and not yet activated in EFI: %#v", created)
	}
	if _, err := os.Stat(filepath.Join(created.Path, "efi", "bootrecov-snapshots")); !os.IsNotExist(err) {
		t.Fatalf("recursive efi backup copy detected, expected no nested efi backup dir, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(created.Path, "efi", "EFI")); !os.IsNotExist(err) {
		t.Fatalf("ESP content should not be copied into full backup, err=%v", err)
	}
}

func TestCreateBootBackupNowArchivesMatchingRootModules(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	version := "6.6.7-arch1-1"
	writeFile(t, filepath.Join(boot, "vmlinuz-"+version))
	writeFile(t, filepath.Join(boot, "initrd.img-"+version))
	writeFile(t, filepath.Join(RootModulesDir, version, "kernel", "fs", "xfs.ko"))

	created, err := CreateBootBackupNow()
	if err != nil {
		t.Fatalf("CreateBootBackupNow failed: %v", err)
	}
	if !created.HasArchivedModules {
		t.Fatalf("expected archived root modules: %#v", created)
	}
	if _, err := os.Stat(archivedModuleImagePath(created.SnapshotPath, version)); err != nil {
		t.Fatalf("expected archived module image: %v", err)
	}
	if err := ensureEFIMirrorFromSnapshot(&created); err != nil {
		t.Fatalf("ensureEFIMirrorFromSnapshot failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(efi, created.Name, bootrecovMetadataRoot)); !os.IsNotExist(err) {
		t.Fatalf("bootrecov metadata should not be copied to EFI mirror, err=%v", err)
	}
}

func TestModelShowsSyncHintAndSyncKeyRepairs(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	makeBootableBackup(t, snap, "activate-me")

	m, err := NewModel()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.status, "without EFI activation") {
		t.Fatalf("expected activation hint, got status: %q", m.status)
	}

	if m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}); m2 != nil {
		m = m2.(Model)
	}
	if len(m.Backups) != 1 || !m.Backups[0].HasSnapshot || m.Backups[0].HasEFI || !m.Backups[0].InSync {
		t.Fatalf("expected snapshot to remain unactivated after s reconcile: %#v", m.Backups)
	}
	if !strings.Contains(m.status, "reconcile complete") {
		t.Fatalf("expected reconcile status, got: %q", m.status)
	}
}

func TestInstallPacmanHookWritesExpectedCommand(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	if err := InstallPacmanHook("/usr/bin/bootrecov"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(PacmanHookPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "Exec = /usr/bin/env BOOTRECOV_ACCEPT_RISK=1 /usr/bin/bootrecov hook backup-now") {
		t.Fatalf("unexpected hook content: %s", text)
	}
	if !strings.Contains(text, "Target = linux*") || !strings.Contains(text, "Target = grub") {
		t.Fatalf("expected boot-critical package targets in hook: %s", text)
	}
}

func TestIsInsufficientSpaceError(t *testing.T) {
	cases := []error{
		fmt.Errorf("%w: need 1GiB, snapshot free=1MiB", ErrInsufficientSnapshotSpace),
		fmt.Errorf("%w", ErrInsufficientEFISpace),
	}
	for _, err := range cases {
		if !IsInsufficientSpaceError(err) {
			t.Fatalf("expected space error for %v", err)
		}
	}
	if IsInsufficientSpaceError(fmt.Errorf("%w", os.ErrPermission)) {
		t.Fatal("permission error should not be treated as space error")
	}
}

func TestFilesystemENOSPCMapsToTypedSpaceErrors(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	snapshotErr := wrapFilesystemWriteError(filepath.Join(snap, "file"), &os.PathError{Op: "write", Path: filepath.Join(snap, "file"), Err: syscall.ENOSPC})
	if !errors.Is(snapshotErr, ErrInsufficientSnapshotSpace) {
		t.Fatalf("expected snapshot space error, got %v", snapshotErr)
	}

	efiErr := wrapFilesystemWriteError(filepath.Join(efi, "file"), &os.PathError{Op: "write", Path: filepath.Join(efi, "file"), Err: syscall.ENOSPC})
	if !errors.Is(efiErr, ErrInsufficientEFISpace) {
		t.Fatalf("expected EFI space error, got %v", efiErr)
	}

	permissionErr := wrapFilesystemWriteError(filepath.Join(snap, "file"), &os.PathError{Op: "write", Path: filepath.Join(snap, "file"), Err: os.ErrPermission})
	if IsInsufficientSpaceError(permissionErr) {
		t.Fatalf("permission error should not be classified as space error: %v", permissionErr)
	}
}

func TestExternalWriteFailureMapsLowFreeSpaceToTypedError(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	setFreeBytes(t, lowFreeSpaceThreshold-1)

	rclone := filepath.Join(t.TempDir(), "rclone")
	writeExecutable(t, rclone, "#!/bin/sh\nexit 23\n")
	RcloneBin = rclone
	RequireRclone = true

	err := runRcloneSync(boot, filepath.Join(snap, "dst"), nil, nil)
	if !errors.Is(err, ErrSyncFailed) {
		t.Fatalf("expected sync error, got %v", err)
	}
	if !errors.Is(err, ErrInsufficientSnapshotSpace) {
		t.Fatalf("expected low free space to classify as snapshot space error, got %v", err)
	}

	mksquashfs := filepath.Join(t.TempDir(), "mksquashfs")
	writeExecutable(t, mksquashfs, "#!/bin/sh\nexit 1\n")
	MksquashfsBin = mksquashfs
	RequireMksquashfs = true

	err = createSquashFSModuleImage(boot, filepath.Join(efi, "modules.sqfs"))
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("expected command error, got %v", err)
	}
	if !errors.Is(err, ErrInsufficientEFISpace) {
		t.Fatalf("expected low free space to classify as EFI space error, got %v", err)
	}
}

func TestInstallPacmanHookRejectsWhitespacePath(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	err := InstallPacmanHook("/usr/local/bin/boot recov")
	if !errors.Is(err, ErrHookExecutablePath) {
		t.Fatalf("expected whitespace path error, got %v", err)
	}
}

func TestInstallPacmanHookRejectsUbuntuUntilAptHookExists(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	activePlatformID = PlatformUbuntu
	activePlatformName = "Ubuntu"
	activeHookSupported = false

	err := InstallPacmanHook("/usr/bin/bootrecov")
	if !errors.Is(err, ErrUnsupportedPackageHook) {
		t.Fatalf("expected planned apt hook error, got %v", err)
	}
}

func TestRecoveryCommandsRequireActivatedBackup(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	makeBootableBackup(t, snap, "cold")
	_, err := RecoveryCommands("cold")
	if !errors.Is(err, ErrBackupNotActivated) {
		t.Fatalf("expected activation error, got %v", err)
	}
}

func TestRecoveryCommandsUseGrubVisiblePaths(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	mountInfoPath = filepath.Join(t.TempDir(), "mountinfo")
	content := fmt.Sprintf("36 25 8:2 / %s rw,relatime - vfat /dev/sda2 rw\n", efi)
	if err := os.WriteFile(mountInfoPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	makeBootableBackup(t, snap, "pair")
	makeBootableBackup(t, efi, "pair")

	commands, err := RecoveryCommands("pair")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"search --file --set=root /pair/vmlinuz",
		"linux /pair/vmlinuz",
		"initrd /pair/initrd.img",
		"boot",
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("expected %q in recovery commands: %s", want, commands)
		}
	}
}

func TestHelpMentionsFlag(t *testing.T) {
	help := `
Flags for copy:
  -l, --links     Translate symlinks
      --times     Preserve time
`
	if !helpMentionsFlag(help, "--links") {
		t.Fatal("expected --links to be detected")
	}
	if helpMentionsFlag(help, "--perms") {
		t.Fatal("did not expect --perms to be detected")
	}
}

func TestRejectsPathTraversalBackupNames(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"../escape", ActivateBackup("../escape")},
		{"../escape", DeactivateBackup("../escape")},
		{"../escape", DeleteBackup("../escape")},
	} {
		if !errors.Is(tc.err, ErrInvalidBackupName) {
			t.Fatalf("expected invalid backup name for %q, got %v", tc.name, tc.err)
		}
	}
	if _, err := RecoveryCommands("../escape"); !errors.Is(err, ErrInvalidBackupName) {
		t.Fatalf("expected invalid backup name for RecoveryCommands, got %v", err)
	}
	if err := AddGrubEntry(BootBackup{Name: "../escape"}); !errors.Is(err, ErrInvalidBackupName) {
		t.Fatalf("expected invalid backup name for AddGrubEntry, got %v", err)
	}
}

func TestActivateBackupRequiresEFIMountWhenEnabled(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	RequireEFIMount = true
	mountInfoPath = filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(mountInfoPath, []byte("24 1 8:1 / / rw,relatime - ext4 /dev/root rw\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeBootableBackup(t, snap, "cold")

	err := ActivateBackup("cold")
	if !errors.Is(err, ErrEFIMountUnavailable) {
		t.Fatalf("expected EFI mount error, got %v", err)
	}
}

func TestActivateBackupAcceptsMountedEFIRoot(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	RequireEFIMount = true
	mountInfoPath = filepath.Join(t.TempDir(), "mountinfo")
	efiRoot := filepath.Dir(efi)
	content := fmt.Sprintf("36 25 8:2 / %s rw,relatime - vfat /dev/sda2 rw\n", efiRoot)
	if err := os.WriteFile(mountInfoPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	makeBootableBackup(t, snap, "cold")

	if err := ActivateBackup("cold"); err != nil {
		t.Fatalf("ActivateBackup failed with mounted EFI root: %v", err)
	}
}

func TestBuildRcloneSyncArgsRespectsSupportedFlags(t *testing.T) {
	supported := map[string]bool{
		"--links":         true,
		"--times":         true,
		"--delete-during": true,
		"--perms":         false,
	}
	args := buildRcloneSyncArgs("/src/", "/dst/", []string{"efi/boot-backups/**"}, nil, supported)
	got := strings.Join(args, " ")
	if strings.Contains(got, "--perms") {
		t.Fatalf("unexpected --perms in args: %q", got)
	}
	for _, want := range []string{"sync", "/src/", "/dst/", "--links", "--times", "--delete-during", "--exclude efi/boot-backups/**"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in args: %q", want, got)
		}
	}
}

func TestBuildRcloneSyncArgsWithIncludesAddsCatchAllExclude(t *testing.T) {
	supported := map[string]bool{
		"--links":         true,
		"--times":         true,
		"--delete-during": true,
		"--perms":         false,
	}
	args := buildRcloneSyncArgs("/src/", "/dst/", []string{"efi/boot-backups/**"}, []string{"vmlinuz*"}, supported)
	got := strings.Join(args, " ")
	for _, want := range []string{"--include vmlinuz*", "--exclude efi/boot-backups/**", "--exclude *"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in args: %q", want, got)
		}
	}
}

func TestGrubInitrdArgsIncludesMicrocodeFirst(t *testing.T) {
	got := grubInitrdArgs("/boot/efi/boot-backups/snap", []string{"intel-ucode.img"}, "initrd.img")
	want := "/boot/efi/boot-backups/snap/intel-ucode.img /boot/efi/boot-backups/snap/initrd.img"
	if got != want {
		t.Fatalf("grubInitrdArgs=%q want %q", got, want)
	}
}

func TestGrubVisiblePathStripsEFIMountPrefix(t *testing.T) {
	mountInfoPath = filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(mountInfoPath, []byte("36 25 8:2 / /boot/efi rw,relatime - vfat /dev/sda2 rw\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := grubVisiblePath("/boot/efi/bootrecov-snapshots/snap")
	want := "/bootrecov-snapshots/snap"
	if got != want {
		t.Fatalf("grubVisiblePath EFI=%q want %q", got, want)
	}
}

func TestGrubVisiblePathStripsBootMountPrefix(t *testing.T) {
	mountInfoPath = filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(mountInfoPath, []byte("35 25 8:1 / /boot rw,relatime - ext4 /dev/sda1 rw\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := grubVisiblePath("/boot/bootrecov-snapshots/snap")
	want := "/bootrecov-snapshots/snap"
	if got != want {
		t.Fatalf("grubVisiblePath boot=%q want %q", got, want)
	}
}

func TestGrubVisiblePathUsesDeepestMountPoint(t *testing.T) {
	mountInfoPath = filepath.Join(t.TempDir(), "mountinfo")
	content := strings.Join([]string{
		"24 1 8:1 / / rw,relatime - ext4 /dev/root rw",
		"35 24 8:2 / /boot rw,relatime - ext4 /dev/sda1 rw",
		"36 35 8:3 / /boot/efi rw,relatime - vfat /dev/sda2 rw",
	}, "\n") + "\n"
	if err := os.WriteFile(mountInfoPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := grubVisiblePath("/boot/efi/bootrecov-snapshots/snap/vmlinuz")
	want := "/bootrecov-snapshots/snap/vmlinuz"
	if got != want {
		t.Fatalf("grubVisiblePath deepest=%q want %q", got, want)
	}
}

func TestAddGrubEntryUsesGrubVisibleBootPaths(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	mountInfoPath = filepath.Join(t.TempDir(), "mountinfo")
	content := fmt.Sprintf("36 25 8:2 / %s rw,relatime - vfat /dev/sda2 rw\n", efi)
	if err := os.WriteFile(mountInfoPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	makeBootableBackup(t, snap, "pair")
	makeBootableBackup(t, efi, "pair")

	if err := AddGrubEntry(BootBackup{Name: "pair"}); err != nil {
		t.Fatalf("AddGrubEntry failed: %v", err)
	}

	data, err := os.ReadFile(grub)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "menuentry 'Bootrecov "+filepath.Join(efi, "pair")+"'") {
		t.Fatalf("expected display path in menuentry title, got: %s", text)
	}
	wantSearch := "search --file --set=root /pair/vmlinuz"
	if !strings.Contains(text, wantSearch) {
		t.Fatalf("expected grub-visible search path %q, got: %s", wantSearch, text)
	}
	wantLinux := "linux /pair/vmlinuz"
	if !strings.Contains(text, wantLinux) {
		t.Fatalf("expected grub-visible linux path %q, got: %s", wantLinux, text)
	}
	wantInitrd := "initrd /pair/initrd.img"
	if !strings.Contains(text, wantInitrd) {
		t.Fatalf("expected grub-visible initrd path %q, got: %s", wantInitrd, text)
	}
}

func TestAddGrubEntryClosesCustomFileBeforeGrubMkconfig(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeBootableBackup(t, snap, "pair")
	makeBootableBackup(t, efi, "pair")

	checker := filepath.Join(t.TempDir(), "check-grub-closed")
	script := fmt.Sprintf(`#!/bin/sh
if fuser %q >/dev/null 2>&1; then
  echo "grub custom file still open"
  exit 126
fi
exit 0
`, grub)
	if err := os.WriteFile(checker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	AutoUpdateGrub = true
	GrubMkconfig = checker

	if err := AddGrubEntry(BootBackup{Name: "pair"}); err != nil {
		t.Fatalf("AddGrubEntry failed: %v", err)
	}
}

func TestParseTimeFromBackupName(t *testing.T) {
	cases := []string{"20260411-1901", "20260411-190145", "snap-20250713-1830"}
	for _, in := range cases {
		if ts, ok := parseTimeFromBackupName(in); !ok || ts.IsZero() {
			t.Fatalf("expected parsable backup time for %q, got %v %v", in, ts, ok)
		}
	}
}

func TestParseKernelVersionFromName(t *testing.T) {
	cases := map[string]string{
		"vmlinuz-6.8.0-31-generic":             "6.8.0-31-generic",
		"initrd.img-6.6.7-arch1-1":             "6.6.7-arch1-1",
		"initramfs-6.6.7-arch1-1.img":          "6.6.7-arch1-1",
		"initramfs-6.6.7-arch1-1-fallback.img": "6.6.7-arch1-1",
		"vmlinuz-linux":                        "",
		"initramfs-linux-fallback":             "",
	}
	for in, want := range cases {
		got := parseKernelVersionFromName(in)
		if got != want {
			t.Fatalf("parseKernelVersionFromName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestFindKernelAndInitramfsPairsMatchingVersions(t *testing.T) {
	base := t.TempDir()
	writeFile(t, filepath.Join(base, "vmlinuz-7.0.0-arch1-1"))
	writeFile(t, filepath.Join(base, "vmlinuz-6.6.7-arch1-1"))
	writeFile(t, filepath.Join(base, "initrd.img-7.0.0-arch1-1"))

	kernel, initrd := findKernelAndInitramfs(base)
	if kernel != "vmlinuz-7.0.0-arch1-1" || initrd != "initrd.img-7.0.0-arch1-1" {
		t.Fatalf("expected matching kernel/initrd pair, got %q %q", kernel, initrd)
	}
}

func TestDiscoverBackupsDetectsMissingRootModuleTree(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeVersionedBootableBackup(t, snap, "old", "6.6.7-arch1-1")
	makeVersionedBootableBackup(t, efi, "old", "6.6.7-arch1-1")

	backups, err := DiscoverBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(backups))
	}
	b := backups[0]
	if !b.RootModulesKnown {
		t.Fatalf("expected root module tree to be checked: %#v", b)
	}
	if b.HasRootModules {
		t.Fatalf("expected root modules to be missing: %#v", b)
	}
	if IsBootReady(b) {
		t.Fatalf("expected backup with missing root modules not to be boot-ready: %#v", b)
	}
}

func TestAddGrubEntryRejectsMissingRootModuleTree(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeVersionedBootableBackup(t, snap, "old", "6.6.7-arch1-1")
	makeVersionedBootableBackup(t, efi, "old", "6.6.7-arch1-1")

	err := AddGrubEntry(BootBackup{Name: "old"})
	if !errors.Is(err, ErrRootModulesMissing) {
		t.Fatalf("expected missing module tree error, got %v", err)
	}
}

func TestActivateBackupDoesNotInstallArchivedRootModules(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	version := "6.6.7-arch1-1"
	makeVersionedBootableBackup(t, snap, "old", version)
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "old"), version))

	err := ActivateBackup("old")
	if !errors.Is(err, ErrArchivedModulesUnsafe) {
		t.Fatalf("expected no root filesystem write error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(RootModulesDir, version)); !os.IsNotExist(statErr) {
		t.Fatalf("activation must not create root module tree, err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(efi, "old")); !os.IsNotExist(statErr) {
		t.Fatalf("activation should not create EFI mirror for unsafe snapshot, err=%v", statErr)
	}
}

func TestAddGrubEntryAllowsMatchingRootModuleTree(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	version := "6.6.7-arch1-1"
	makeVersionedBootableBackup(t, snap, "old", version)
	makeVersionedBootableBackup(t, efi, "old", version)
	if err := os.MkdirAll(filepath.Join(RootModulesDir, version), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := AddGrubEntry(BootBackup{Name: "old"}); err != nil {
		t.Fatalf("AddGrubEntry failed: %v", err)
	}
	entries, err := ListGrubEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one GRUB entry, got %#v", entries)
	}
}

func TestDeleteBackupRemovesBothCopiesAndGrubEntry(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	makeBootableBackup(t, snap, "delme")
	makeBootableBackup(t, efi, "delme")
	if err := AddGrubEntry(BootBackup{Name: "delme"}); err != nil {
		t.Fatal(err)
	}

	if err := DeleteBackup("delme"); err != nil {
		t.Fatalf("DeleteBackup failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snap, "delme")); !os.IsNotExist(err) {
		t.Fatalf("snapshot dir still exists, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(efi, "delme")); !os.IsNotExist(err) {
		t.Fatalf("efi dir still exists, err=%v", err)
	}
	entries, err := ListGrubEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no grub entries after delete, got %#v", entries)
	}
}

func TestModelDeleteRequiresConfirmation(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeBootableBackup(t, snap, "keep")
	makeBootableBackup(t, efi, "keep")

	m, err := NewModel()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(m.Backups))
	}
	if m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}); m2 != nil {
		m = m2.(Model)
	}
	if !m.confirmDelete || m.deleteTarget != "keep" {
		t.Fatalf("expected delete confirmation state, got %#v", m)
	}
	if m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}); m2 != nil {
		m = m2.(Model)
	}
	if m.confirmDelete {
		t.Fatal("expected delete confirmation to be canceled")
	}
	if _, err := os.Stat(filepath.Join(snap, "keep")); err != nil {
		t.Fatalf("backup should still exist after cancel: %v", err)
	}
	if m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}); m2 != nil {
		m = m2.(Model)
	}
	if m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}); m2 != nil {
		m = m2.(Model)
	}
	if len(m.Backups) != 0 {
		t.Fatalf("expected backup list empty after confirmed delete, got %#v", m.Backups)
	}
	if _, err := os.Stat(filepath.Join(snap, "keep")); !os.IsNotExist(err) {
		t.Fatalf("backup should be deleted after confirm, err=%v", err)
	}
}

func TestCheckBackupSpaceInsufficient(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFile(t, filepath.Join(boot, "vmlinuz"))
	writeFile(t, filepath.Join(boot, "initrd.img"))
	setFreeBytes(t, 1)

	if err := checkSnapshotSpace(); !errors.Is(err, ErrInsufficientSnapshotSpace) {
		t.Fatalf("expected insufficient space error, got %v", err)
	}
}

func TestMaxBackupCountFromFree(t *testing.T) {
	tests := []struct {
		name              string
		efiFree, estimate int64
		want              int64
	}{
		{name: "balanced", efiFree: 1000, estimate: 100, want: 10},
		{name: "efi limited", efiFree: 900, estimate: 100, want: 9},
		{name: "small free", efiFree: 450, estimate: 100, want: 4},
		{name: "zero estimate", efiFree: 1000, estimate: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maxBackupCountFromFree(tt.efiFree, tt.estimate)
			if got != tt.want {
				t.Fatalf("maxBackupCountFromFree=%d want %d", got, tt.want)
			}
		})
	}
}
