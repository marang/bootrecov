package tui

import (
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
	oldBoot, oldSnap, oldEFI, oldGrub, oldGrubCfg, oldMkconfig, oldAutoGrub, oldHookPath, oldRclone, oldRequire, oldStatfs, oldMountInfo :=
		BootDir, SnapshotDir, EfiDir, GrubCustom, GrubCfgOutput, GrubMkconfig, AutoUpdateGrub, PacmanHookPath, RcloneBin, RequireRclone, statfsFunc, mountInfoPath
	BootDir, SnapshotDir, EfiDir, GrubCustom = boot, snap, efi, grub
	GrubCfgOutput = filepath.Join(filepath.Dir(grub), "grub.cfg")
	GrubMkconfig = ""
	AutoUpdateGrub = false
	PacmanHookPath = filepath.Join(filepath.Dir(grub), "bootrecov.hook")
	RcloneBin = ""
	RequireRclone = false
	statfsFunc = syscall.Statfs
	t.Cleanup(func() {
		BootDir, SnapshotDir, EfiDir, GrubCustom, GrubCfgOutput, GrubMkconfig, AutoUpdateGrub, PacmanHookPath, RcloneBin, RequireRclone, statfsFunc, mountInfoPath =
			oldBoot, oldSnap, oldEFI, oldGrub, oldGrubCfg, oldMkconfig, oldAutoGrub, oldHookPath, oldRclone, oldRequire, oldStatfs, oldMountInfo
	})
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

func makeBootableBackup(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "vmlinuz"))
	writeFile(t, filepath.Join(dir, "initrd.img"))
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

	err := CheckRuntimeDependencies()
	if err == nil {
		t.Fatal("expected missing dependency error")
	}
	text := err.Error()
	if !strings.Contains(text, "definitely-missing-rclone") {
		t.Fatalf("expected rclone in error, got: %s", text)
	}
	if !strings.Contains(text, "definitely-missing-grub-mkconfig") {
		t.Fatalf("expected grub-mkconfig in error, got: %s", text)
	}
}

func TestNewModelFailsEarlyWhenDependenciesMissing(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	RequireRclone = true
	RcloneBin = "definitely-missing-rclone"

	_, err := NewModel()
	if err == nil || !strings.Contains(err.Error(), "required dependencies are missing") {
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

func TestAddGrubEntryRequiresSyncedPair(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	makeBootableBackup(t, efi, "only-efi")
	err := AddGrubEntry(BootBackup{Name: "only-efi", Path: filepath.Join(efi, "only-efi")})
	if err == nil || !strings.Contains(err.Error(), "not activated in EFI") {
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
	if !strings.Contains(text, "Exec = /usr/bin/bootrecov backup-now") {
		t.Fatalf("unexpected hook content: %s", text)
	}
	if !strings.Contains(text, "Target = linux*") || !strings.Contains(text, "Target = grub") {
		t.Fatalf("expected boot-critical package targets in hook: %s", text)
	}
}

func TestRecoveryCommandsRequireActivatedBackup(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	makeBootableBackup(t, snap, "cold")
	_, err := RecoveryCommands("cold")
	if err == nil || !strings.Contains(err.Error(), "not activated in EFI") {
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
      --links     Translate symlinks
      --times     Preserve time
`
	if !helpMentionsFlag(help, "--links") {
		t.Fatal("expected --links to be detected")
	}
	if helpMentionsFlag(help, "--perms") {
		t.Fatal("did not expect --perms to be detected")
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
		"vmlinuz-6.8.0-31-generic": "6.8.0-31-generic",
		"initrd.img-6.6.7-arch1-1": "6.6.7-arch1-1",
		"vmlinuz-linux":            "",
		"initramfs-linux-fallback": "",
	}
	for in, want := range cases {
		got := parseKernelVersionFromName(in)
		if got != want {
			t.Fatalf("parseKernelVersionFromName(%q)=%q want %q", in, got, want)
		}
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
	old := statfsFunc
	statfsFunc = func(_ string, st *syscall.Statfs_t) error {
		st.Bavail = 1
		st.Bsize = 1
		return nil
	}
	t.Cleanup(func() { statfsFunc = old })

	if err := checkSnapshotSpace(); err == nil {
		t.Fatal("expected insufficient space error")
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
