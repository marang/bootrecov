package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	oldBoot, oldSnap, oldEFI, oldGrub, oldGrubCfg, oldMkconfig, oldAutoGrub, oldModules, oldHookPath, oldPostHookPath, oldMkinitcpioInstall, oldMkinitcpioHook, oldMkinitcpioConf, oldMkinitcpioBin, oldUpdateInitramfs, oldRclone, oldRequire, oldMksquashfs, oldRequireMksquashfs, oldUnsquashfs, oldRequireUnsquashfs, oldRequireEFIMount, oldCreateImage, oldRestoreModules, oldStatfs, oldMountInfo, oldKernelCmdline, oldExecLookPath, oldOSReleasePath, oldGrubDefaultPath, oldPlatformOverride, oldBootloaderOverride, oldActivePlatformID, oldActivePlatformName, oldActiveHookSupported, oldActiveBootloaderID, oldActiveBootloaderName, oldActiveWarnings :=
		BootDir, SnapshotDir, EfiDir, GrubCustom, GrubCfgOutput, GrubMkconfig, AutoUpdateGrub, RootModulesDir, PacmanHookPath, PacmanPostHookPath, MkinitcpioInstallPath, MkinitcpioHookPath, MkinitcpioConfPath, MkinitcpioBin, UpdateInitramfs, RcloneBin, RequireRclone, MksquashfsBin, RequireMksquashfs, UnsquashfsBin, RequireUnsquashfs, RequireEFIMount, createModuleImageFunc, restoreModuleTreeFunc, statfsFunc, mountInfoPath, kernelCmdlinePath, execLookPath, OSReleasePath, GrubDefaultPath, PlatformOverride, BootloaderOverride, activePlatformID, activePlatformName, activeHookSupported, activeBootloaderID, activeBootloaderName, activeWarnings
	BootDir, SnapshotDir, EfiDir, GrubCustom = boot, snap, efi, grub
	GrubCfgOutput = filepath.Join(filepath.Dir(grub), "grub.cfg")
	GrubMkconfig = ""
	AutoUpdateGrub = false
	RootModulesDir = filepath.Join(filepath.Dir(grub), "modules")
	PacmanHookPath = filepath.Join(filepath.Dir(grub), "bootrecov.hook")
	PacmanPostHookPath = filepath.Join(filepath.Dir(grub), "bootrecov-post.hook")
	MkinitcpioInstallPath = filepath.Join(filepath.Dir(grub), "initcpio", "install", "bootrecov")
	MkinitcpioHookPath = filepath.Join(filepath.Dir(grub), "initcpio", "hooks", "bootrecov")
	MkinitcpioConfPath = filepath.Join(filepath.Dir(grub), "mkinitcpio.conf")
	MkinitcpioBin = ""
	UpdateInitramfs = false
	RcloneBin = ""
	RequireRclone = false
	MksquashfsBin = ""
	RequireMksquashfs = false
	UnsquashfsBin = ""
	RequireUnsquashfs = false
	RequireEFIMount = false
	createModuleImageFunc = fakeCreateModuleImage
	restoreModuleTreeFunc = fakeRestoreModuleTree
	statfsFunc = syscall.Statfs
	kernelCmdlinePath = filepath.Join(filepath.Dir(grub), "cmdline")
	execLookPath = func(file string) (string, error) {
		if file == "mkinitcpio" {
			return filepath.Join(filepath.Dir(grub), "bin", "mkinitcpio"), nil
		}
		return exec.LookPath(file)
	}
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
	writeFileWithContent(t, MkinitcpioConfPath, "HOOKS=(base udev autodetect modconf block filesystems keyboard fsck)\n")
	t.Cleanup(func() {
		BootDir, SnapshotDir, EfiDir, GrubCustom, GrubCfgOutput, GrubMkconfig, AutoUpdateGrub, RootModulesDir, PacmanHookPath, PacmanPostHookPath, MkinitcpioInstallPath, MkinitcpioHookPath, MkinitcpioConfPath, MkinitcpioBin, UpdateInitramfs, RcloneBin, RequireRclone, MksquashfsBin, RequireMksquashfs, UnsquashfsBin, RequireUnsquashfs, RequireEFIMount, createModuleImageFunc, restoreModuleTreeFunc, statfsFunc, mountInfoPath, kernelCmdlinePath, execLookPath, OSReleasePath, GrubDefaultPath, PlatformOverride, BootloaderOverride, activePlatformID, activePlatformName, activeHookSupported, activeBootloaderID, activeBootloaderName, activeWarnings =
			oldBoot, oldSnap, oldEFI, oldGrub, oldGrubCfg, oldMkconfig, oldAutoGrub, oldModules, oldHookPath, oldPostHookPath, oldMkinitcpioInstall, oldMkinitcpioHook, oldMkinitcpioConf, oldMkinitcpioBin, oldUpdateInitramfs, oldRclone, oldRequire, oldMksquashfs, oldRequireMksquashfs, oldUnsquashfs, oldRequireUnsquashfs, oldRequireEFIMount, oldCreateImage, oldRestoreModules, oldStatfs, oldMountInfo, oldKernelCmdline, oldExecLookPath, oldOSReleasePath, oldGrubDefaultPath, oldPlatformOverride, oldBootloaderOverride, oldActivePlatformID, oldActivePlatformName, oldActiveHookSupported, oldActiveBootloaderID, oldActiveBootloaderName, oldActiveWarnings
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

func fakeRestoreModuleTree(src, dst string) error {
	if !fileExists(src) {
		return fmt.Errorf("%w: module archive does not exist: %s", ErrSourceDirectoryMissing, src)
	}
	if dirExists(dst) {
		return nil
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, "modules.dep"), []byte("restored"), 0o644)
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

func TestConfigureDetectedEnvironmentDetectsMkinitcpioLayout(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	base := filepath.Dir(grub)
	detectedConf := filepath.Join(base, "detected", "mkinitcpio.conf")
	detectedInstall := filepath.Join(base, "detected", "install")
	detectedHooks := filepath.Join(base, "detected", "hooks")
	writeFileWithContent(t, detectedConf, "HOOKS=(base filesystems)\n")
	if err := os.MkdirAll(detectedInstall, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(detectedHooks, 0o755); err != nil {
		t.Fatal(err)
	}
	MkinitcpioConfPath = detectedConf
	MkinitcpioInstallPath = filepath.Join(detectedInstall, "placeholder")
	MkinitcpioHookPath = filepath.Join(detectedHooks, "placeholder")
	MkinitcpioBin = "mkinitcpio"
	execLookPath = func(file string) (string, error) {
		if file == "mkinitcpio" {
			return filepath.Join(base, "bin", "mkinitcpio"), nil
		}
		return exec.LookPath(file)
	}

	info := ConfigureDetectedEnvironment()
	if info.Layout.MkinitcpioConfig != detectedConf {
		t.Fatalf("expected detected mkinitcpio config, got %#v", info.Layout)
	}
	if info.Layout.MkinitcpioInstallHook != filepath.Join(detectedInstall, "bootrecov") {
		t.Fatalf("expected detected install hook dir, got %#v", info.Layout)
	}
	if info.Layout.MkinitcpioRuntimeHook != filepath.Join(detectedHooks, "bootrecov") {
		t.Fatalf("expected detected runtime hook dir, got %#v", info.Layout)
	}
	if MkinitcpioBin != filepath.Join(base, "bin", "mkinitcpio") {
		t.Fatalf("expected mkinitcpio binary detection, got %q", MkinitcpioBin)
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
	if !strings.Contains(strings.Join(info.Warnings, "\n"), "BOOTRECOV_BOOTLOADER=grub") {
		t.Fatalf("expected warning to explain explicit bootloader selection, got %#v", info.Warnings)
	}
}

func TestConfigureDetectedEnvironmentOverrideSelectsGrubWhenSignalsAreAmbiguous(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	if err := os.MkdirAll(filepath.Join(filepath.Dir(efi), "loader", "entries"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, GrubCfgOutput)
	BootloaderOverride = BootloaderGRUB

	info := ConfigureDetectedEnvironment()
	if info.BootloaderID != BootloaderGRUB || !info.BootloaderSupported {
		t.Fatalf("expected explicit grub override to select supported bootloader, got %#v", info)
	}
	for _, warning := range info.Warnings {
		if strings.Contains(warning, "multiple bootloader signals") {
			t.Fatalf("override should suppress ambiguity warning, got %#v", info.Warnings)
		}
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

func writeFileWithContent(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
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

func TestRunCommandCombinedOutputStreamsCommandLines(t *testing.T) {
	script := filepath.Join(t.TempDir(), "emit-lines")
	writeExecutable(t, script, "#!/bin/sh\nprintf 'stdout line\\n'\nprintf 'stderr line\\n' >&2\n")
	var got []string
	var gotMu sync.Mutex
	result := withCommandOutputSink(func(line string) {
		gotMu.Lock()
		defer gotMu.Unlock()
		got = append(got, line)
	}, func() struct {
		out []byte
		err error
	} {
		out, err := runCommandCombinedOutput(exec.Command(script))
		return struct {
			out []byte
			err error
		}{out: out, err: err}
	})
	if result.err != nil {
		t.Fatal(result.err)
	}
	outText := string(result.out)
	for _, want := range []string{"stdout line", "stderr line"} {
		if !strings.Contains(outText, want) {
			t.Fatalf("expected combined output to contain %q, got %q", want, outText)
		}
		found := false
		gotMu.Lock()
		for _, line := range got {
			if line == want {
				found = true
			}
		}
		gotMu.Unlock()
		if !found {
			gotMu.Lock()
			lines := append([]string{}, got...)
			gotMu.Unlock()
			t.Fatalf("expected streamed line %q, got %#v", want, lines)
		}
	}
}

func drainModelCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, batchCmd := range msg {
			if batchCmd == nil {
				continue
			}
			batchMsg := batchCmd()
			if _, ok := batchMsg.(activityTickMsg); ok {
				continue
			}
			if updated, next := m.Update(batchMsg); updated != nil {
				m = updated.(Model)
				if next != nil {
					m = drainModelCmd(t, m, next)
				}
			}
		}
	case activityTickMsg:
		return m
	default:
		if updated, next := m.Update(msg); updated != nil {
			m = updated.(Model)
			if next != nil {
				m = drainModelCmd(t, m, next)
			}
		}
	}
	return m
}

func keyPress(s string) tea.KeyPressMsg {
	switch s {
	case "tab":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab})
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})
	case "up":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	case "down":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	}
	runes := []rune(s)
	if len(runes) == 0 {
		return tea.KeyPressMsg(tea.Key{})
	}
	if len(runes) == 1 {
		return tea.KeyPressMsg(tea.Key{Text: s, Code: runes[0]})
	}
	return tea.KeyPressMsg(tea.Key{Code: runes[0]})
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
	RequireUnsquashfs = true
	UnsquashfsBin = "definitely-missing-unsquashfs"

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
	for _, want := range []string{"definitely-missing-rclone", "definitely-missing-grub-mkconfig", "definitely-missing-mksquashfs", "definitely-missing-unsquashfs"} {
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

func TestSyncBackupsAndGrubRestoresArchivedModulesForActiveEntry(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	version := "6.6.7-arch1-1"
	makeVersionedBootableBackup(t, snap, "active", version)
	makeVersionedBootableBackup(t, efi, "active", version)
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "active"), version))

	entryID := backupIDForName("active")
	entry := fmt.Sprintf("#!/bin/bash\ncat <<'EOF'\nmenuentry 'Bootrecov %s' --id %s {\n}\nEOF\n", filepath.Join(efi, "active"), entryID)
	if err := os.WriteFile(grub, []byte(entry), 0o755); err != nil {
		t.Fatal(err)
	}

	backups, entries, err := SyncBackupsAndGrub()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || !backups[0].HasRootModules {
		t.Fatalf("expected archived modules to be restored for active entry: %#v", backups)
	}
	if _, statErr := os.Stat(filepath.Join(RootModulesDir, version, "modules.dep")); statErr != nil {
		t.Fatalf("reconcile should restore archived root module tree, err=%v", statErr)
	}
	if len(entries) != 1 || entries[0].ID != entryID {
		t.Fatalf("expected active grub entry to remain, got %#v", entries)
	}
}

func TestSyncBackupsAndGrubRestoresModulesRemovedAfterActivation(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	version := "6.6.7-arch1-1"
	makeVersionedBootableBackup(t, snap, "active", version)
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "active"), version))
	writeFile(t, filepath.Join(RootModulesDir, version, "modules.dep"))

	if err := ActivateBackup("active"); err != nil {
		t.Fatalf("ActivateBackup failed: %v", err)
	}
	if !dirExists(filepath.Join(RootModulesDir, version)) {
		t.Fatalf("expected activated fallback to start boot-ready")
	}

	if err := os.RemoveAll(filepath.Join(RootModulesDir, version)); err != nil {
		t.Fatal(err)
	}
	backups, entries, err := SyncBackupsAndGrub()
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(RootModulesDir, version, "modules.dep")); statErr != nil {
		t.Fatalf("post-update reconcile should restore modules removed after activation, err=%v", statErr)
	}
	if len(backups) != 1 || !IsBootReady(backups[0]) {
		t.Fatalf("expected active fallback to be boot-ready again after reconcile: %#v", backups)
	}
	if len(entries) != 1 || entries[0].Name != "active" {
		t.Fatalf("expected active grub entry to remain, got %#v", entries)
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

func TestCreateBootBackupNowRejectsUnreadableRequiredBootArtifact(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFile(t, filepath.Join(boot, "vmlinuz"))
	writeFile(t, filepath.Join(boot, "initrd.img"))
	if err := os.Chmod(filepath.Join(boot, "initrd.img"), 0); err != nil {
		t.Fatal(err)
	}

	err := validateBootSourceForSnapshot()
	if err == nil {
		t.Skip("test process can read mode 000 files")
	}

	_, err = CreateBootBackupNow()
	if !errors.Is(err, ErrBackupIncomplete) {
		t.Fatalf("expected unreadable artifact to be rejected as incomplete, got %v", err)
	}
	entries, readErr := os.ReadDir(snap)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("failed backup should not leave discoverable snapshots: %#v", entries)
	}
}

func TestCreateBootBackupNowCleansStagingAfterCopyFailure(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFile(t, filepath.Join(boot, "vmlinuz"))
	writeFile(t, filepath.Join(boot, "initrd.img"))
	writeFile(t, filepath.Join(boot, "grub", "grub.cfg"))
	secret := filepath.Join(boot, "grub", "secret.cfg")
	writeFile(t, secret)
	if err := os.Chmod(secret, 0); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(secret); err == nil {
		_ = f.Close()
		t.Skip("test process can read mode 000 files")
	}

	_, err := CreateBootBackupNow()
	if err == nil {
		t.Fatal("expected copy failure")
	}
	entries, readErr := os.ReadDir(snap)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("failed backup should clean staging directory, found %#v", entries)
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

	if m2, cmd := m.Update(keyPress("s")); m2 != nil {
		m = m2.(Model)
		m = drainModelCmd(t, m, cmd)
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

	if HookInstalled() {
		t.Fatal("hook should start uninstalled")
	}
	if err := InstallPacmanHook("/usr/bin/bootrecov"); err != nil {
		t.Fatal(err)
	}
	if !HookInstalled() {
		t.Fatal("hook should be detected after install")
	}
	preData, err := os.ReadFile(PacmanHookPath)
	if err != nil {
		t.Fatal(err)
	}
	preText := string(preData)
	if !strings.Contains(preText, "When = PreTransaction") || !strings.Contains(preText, "Exec = /usr/bin/env BOOTRECOV_ACCEPT_RISK=1 /usr/bin/bootrecov hook backup-now") {
		t.Fatalf("unexpected pre hook content: %s", preText)
	}
	if !strings.Contains(preText, "Target = linux*") || !strings.Contains(preText, "Target = grub") {
		t.Fatalf("expected boot-critical package targets in pre hook: %s", preText)
	}
	postData, err := os.ReadFile(PacmanPostHookPath)
	if err != nil {
		t.Fatal(err)
	}
	postText := string(postData)
	if !strings.Contains(postText, "When = PostTransaction") || !strings.Contains(postText, "Exec = /usr/bin/env BOOTRECOV_ACCEPT_RISK=1 /usr/bin/bootrecov hook reconcile-active") {
		t.Fatalf("unexpected post hook content: %s", postText)
	}
	if !strings.Contains(postText, "Target = linux*") || !strings.Contains(postText, "Target = grub") {
		t.Fatalf("expected boot-critical package targets in post hook: %s", postText)
	}
	installData, err := os.ReadFile(MkinitcpioInstallPath)
	if err != nil {
		t.Fatal(err)
	}
	installText := string(installData)
	if !strings.Contains(installText, "add_binary /usr/bin/unsquashfs") || !strings.Contains(installText, "add_runscript") {
		t.Fatalf("unexpected mkinitcpio install hook content: %s", installText)
	}
	runtimeData, err := os.ReadFile(MkinitcpioHookPath)
	if err != nil {
		t.Fatal(err)
	}
	runtimeText := string(runtimeData)
	for _, want := range []string{"run_latehook()", "bootrecov_entry=", "unsquashfs -d", "/new_root", ".bootrecov/root-modules"} {
		if !strings.Contains(runtimeText, want) {
			t.Fatalf("expected runtime hook to contain %q: %s", want, runtimeText)
		}
	}
	confData, err := os.ReadFile(MkinitcpioConfPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(confData), "filesystems bootrecov keyboard") {
		t.Fatalf("expected bootrecov hook after filesystems in mkinitcpio config: %s", string(confData))
	}
}

func TestModelShowsProgressWhileTaskRuns(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFile(t, filepath.Join(boot, "vmlinuz"))
	writeFile(t, filepath.Join(boot, "initrd.img"))

	m, err := NewModel()
	if err != nil {
		t.Fatal(err)
	}
	m.status = "ready"
	idleView := m.viewString()
	idleLines := strings.Count(idleView, "\n")
	updated, cmd := m.Update(keyPress("b"))
	if updated == nil || cmd == nil {
		t.Fatal("expected backup key to start an async task")
	}
	m = updated.(Model)
	if !m.busy || m.task != taskBackup {
		t.Fatalf("expected model to be busy creating backup, got %#v", m)
	}
	view := m.viewString()
	if !strings.Contains(view, "creating snapshot and module archive") {
		t.Fatalf("expected progress line in view:\n%s", view)
	}
	if strings.Count(view, "creating snapshot and module archive") != 1 {
		t.Fatalf("expected task label only once in busy view:\n%s", view)
	}
	updated, _ = m.Update(taskOutputMsg{line: "rclone: Transferred: 622.9 MiB / 622.9 MiB", ok: true})
	m = updated.(Model)
	if !strings.Contains(m.viewString(), "rclone: Transferred") {
		t.Fatalf("expected command output in task detail line:\n%s", m.viewString())
	}
	if busyLines := strings.Count(view, "\n"); busyLines != idleLines {
		t.Fatalf("progress row should be reserved, idle lines=%d busy lines=%d\nidle:\n%s\nbusy:\n%s", idleLines, busyLines, idleView, view)
	}
	m = drainModelCmd(t, m, cmd)
	if m.busy {
		t.Fatal("expected task completion to clear busy state")
	}
	if len(m.Backups) != 1 {
		t.Fatalf("expected created backup after draining task, got %#v", m.Backups)
	}
	completeView := m.viewString()
	if strings.Count(completeView, "snapshot created") != 1 {
		t.Fatalf("expected completion text once beside completed bar:\n%s", completeView)
	}
	if !strings.Contains(completeView, "━") {
		t.Fatalf("expected completed status row to include filled activity bar:\n%s", completeView)
	}
}

func TestModelDoesNotQuitWhileTaskRuns(t *testing.T) {
	m := newModelComponents()
	m.busy = true
	m.task = taskInstallHook
	m.taskLabel = "installing hooks and rebuilding initramfs (mkinitcpio -P)"
	updated, cmd := m.Update(keyPress("q"))
	if cmd != nil {
		t.Fatal("busy q should not quit or start a command")
	}
	m = updated.(Model)
	if !m.busy {
		t.Fatal("busy q should keep operation running")
	}
	if !strings.Contains(m.viewString(), "operation is still running") {
		t.Fatalf("expected wait warning in task detail:\n%s", m.viewString())
	}
	if strings.Contains(m.viewString(), "q quit") {
		t.Fatalf("busy footer should not advertise quit:\n%s", m.viewString())
	}
}

func TestModelHookKeyTogglesInstallAndUninstall(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	m, err := NewModel()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.viewString(), "install hook") || !strings.Contains(m.viewString(), "Hook: OFF") {
		t.Fatalf("expected install hook footer before install:\n%s", m.viewString())
	}

	updated, cmd := m.Update(keyPress("p"))
	if updated == nil || cmd == nil {
		t.Fatal("expected hook key to start install task")
	}
	m = updated.(Model)
	if !m.busy || !strings.Contains(m.viewString(), "mkinitcpio -P") || strings.Contains(m.viewString(), "can take a while") {
		t.Fatalf("expected hook install to show initramfs rebuild progress:\n%s", m.viewString())
	}
	updated, _ = m.Update(taskOutputMsg{line: "==> Building image from preset: /etc/mkinitcpio.d/linux.preset", ok: true})
	m = updated.(Model)
	if !strings.Contains(m.viewString(), "Building image from preset") {
		t.Fatalf("expected mkinitcpio output in task detail line:\n%s", m.viewString())
	}
	m = drainModelCmd(t, m, cmd)
	if !HookInstalled() {
		t.Fatal("expected hook to be installed after first toggle")
	}
	if !strings.Contains(m.viewString(), "uninstall hook") || !strings.Contains(m.viewString(), "Hook: ON") {
		t.Fatalf("expected uninstall hook footer after install:\n%s", m.viewString())
	}

	updated, cmd = m.Update(keyPress("p"))
	if updated == nil || cmd == nil {
		t.Fatal("expected hook key to start uninstall task")
	}
	m = updated.(Model)
	if !m.busy || !strings.Contains(m.viewString(), "mkinitcpio -P") || strings.Contains(m.viewString(), "can take a while") {
		t.Fatalf("expected hook uninstall to show initramfs rebuild progress:\n%s", m.viewString())
	}
	m = drainModelCmd(t, m, cmd)
	if HookInstalled() {
		t.Fatal("expected hook to be uninstalled after second toggle")
	}
	if !strings.Contains(m.viewString(), "install hook") || !strings.Contains(m.viewString(), "Hook: OFF") {
		t.Fatalf("expected install hook footer after uninstall:\n%s", m.viewString())
	}
}

func TestModelHelpToggleAndTabViews(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	makeBootableBackup(t, snap, "pair")
	makeBootableBackup(t, efi, "pair")
	if err := AddGrubEntry(BootBackup{Name: "pair"}); err != nil {
		t.Fatal(err)
	}

	m, err := NewModel()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.viewString(), "Backups") || !strings.Contains(m.viewString(), "Bootloader") {
		t.Fatalf("expected tab labels in backup view:\n%s", m.viewString())
	}
	if !strings.Contains(m.viewString(), "delete") {
		t.Fatalf("expected delete action in short help:\n%s", m.viewString())
	}
	if strings.Contains(m.viewString(), "recovery cmds") {
		t.Fatalf("short help should not show full backup command set:\n%s", m.viewString())
	}
	updated, _ := m.Update(keyPress("?"))
	m = updated.(Model)
	if !m.help.ShowAll || !strings.Contains(m.viewString(), "recovery cmds") {
		t.Fatalf("expected ? to show full help:\n%s", m.viewString())
	}
	updated, _ = m.Update(keyPress("tab"))
	m = updated.(Model)
	if m.mode != modeEntries || !m.entryTable.Focused() {
		t.Fatalf("expected tab to focus bootloader entries, got mode=%v focused=%v", m.mode, m.entryTable.Focused())
	}
	if !strings.Contains(m.viewString(), "Bootloader Entries") || !strings.Contains(m.viewString(), "pair") {
		t.Fatalf("expected bootloader table after tab:\n%s", m.viewString())
	}
}

func TestModelUsesCompactComponentHeights(t *testing.T) {
	if got := compactBackupListHeight(3, 60); got != 10 {
		t.Fatalf("expected compact height for 3 backups, got %d", got)
	}
	if got := compactBackupListHeight(30, 60); got != 14 {
		t.Fatalf("expected backup height cap, got %d", got)
	}
	if got := compactEntryTableHeight(2, 60); got != 4 {
		t.Fatalf("expected compact table height for 2 entries, got %d", got)
	}
	if got := compactEntryTableHeight(30, 60); got != 10 {
		t.Fatalf("expected table height cap, got %d", got)
	}
}

func TestActivityBarSweepsLeftToRight(t *testing.T) {
	first := activityBar(0, 16)
	middle := activityBar(8, 16)
	reset := activityBar(21, 16)
	if first == middle {
		t.Fatalf("activity bar should animate between frames:\n%s\n%s", first, middle)
	}
	if first != reset {
		t.Fatalf("activity bar should restart after one left-to-right sweep:\nfirst: %s\nreset: %s", first, reset)
	}
	if strings.Contains(first, "█") || strings.Contains(middle, "█") {
		t.Fatalf("activity bar should use quiet line glyphs, got:\n%s\n%s", first, middle)
	}
}

func TestStatusActivityLineUsesCompletedBar(t *testing.T) {
	m := newModelComponents()
	m.width = 100
	m = m.resizeComponents()
	m.status = "snapshot created: test"
	line := statusActivityLine(m)
	if !strings.Contains(line, "snapshot created: test") {
		t.Fatalf("expected status text beside completed bar: %q", line)
	}
	if got := lipgloss.Width(line); got != m.activityWidth {
		t.Fatalf("expected status line to fill activity width %d, got %d: %q", m.activityWidth, got, line)
	}
	if !strings.Contains(line, "━") {
		t.Fatalf("expected completed bar in status line: %q", line)
	}
	if strings.Contains(line, "\x1b[48;") {
		t.Fatalf("status activity line should not use background color: %q", line)
	}
}

func TestBusyActivityLineRightAlignsLabel(t *testing.T) {
	m := newModelComponents()
	m.width = 90
	m = m.resizeComponents()
	m.busy = true
	m.taskLabel = "creating snapshot and module archive"
	line := statusActivityLine(m)
	if !strings.Contains(line, m.taskLabel) {
		t.Fatalf("expected task label in activity line: %q", line)
	}
	if got := lipgloss.Width(line); got != m.activityWidth {
		t.Fatalf("expected busy line to fill activity width %d, got %d: %q", m.activityWidth, got, line)
	}
}

func TestUninstallPacmanHookRemovesHookWhenPresent(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	if err := InstallPacmanHook("/usr/bin/bootrecov"); err != nil {
		t.Fatal(err)
	}
	removed, err := UninstallPacmanHook()
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected hook to be removed")
	}
	if _, err := os.Stat(PacmanHookPath); !os.IsNotExist(err) {
		t.Fatalf("expected pre hook path to be gone, err=%v", err)
	}
	if _, err := os.Stat(PacmanPostHookPath); !os.IsNotExist(err) {
		t.Fatalf("expected post hook path to be gone, err=%v", err)
	}
	if _, err := os.Stat(MkinitcpioInstallPath); !os.IsNotExist(err) {
		t.Fatalf("expected mkinitcpio install hook to be gone, err=%v", err)
	}
	if _, err := os.Stat(MkinitcpioHookPath); !os.IsNotExist(err) {
		t.Fatalf("expected mkinitcpio runtime hook to be gone, err=%v", err)
	}
	confData, err := os.ReadFile(MkinitcpioConfPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(confData), "bootrecov") {
		t.Fatalf("expected bootrecov to be removed from mkinitcpio config: %s", string(confData))
	}
}

func TestUninstallPacmanHookIsIdempotent(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)

	removed, err := UninstallPacmanHook()
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("missing hook should not report removed")
	}
}

func TestInstallPacmanHookRollsBackOnInitramfsFailure(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	UpdateInitramfs = true
	MkinitcpioBin = filepath.Join(t.TempDir(), "mkinitcpio")
	writeExecutable(t, MkinitcpioBin, "#!/bin/sh\nexit 1\n")

	err := InstallPacmanHook("/usr/bin/bootrecov")
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("expected mkinitcpio command error, got %v", err)
	}
	for _, path := range []string{PacmanHookPath, PacmanPostHookPath, MkinitcpioInstallPath, MkinitcpioHookPath} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("expected failed install to roll back %s, stat err=%v", path, statErr)
		}
	}
	confData, readErr := os.ReadFile(MkinitcpioConfPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(confData), "bootrecov") {
		t.Fatalf("failed install should roll back mkinitcpio config: %s", string(confData))
	}
}

func TestUpdateMkinitcpioHooksInsertsAfterFilesystems(t *testing.T) {
	input := []byte("MODULES=()\nHOOKS=(base udev block filesystems keyboard fsck)\n")
	got, changed := updateMkinitcpioHooks(input, true)
	if !changed {
		t.Fatal("expected mkinitcpio config to change")
	}
	if !strings.Contains(string(got), "HOOKS=(base udev block filesystems bootrecov keyboard fsck)") {
		t.Fatalf("unexpected mkinitcpio config:\n%s", string(got))
	}
	gotAgain, changedAgain := updateMkinitcpioHooks(got, true)
	if changedAgain {
		t.Fatalf("bootrecov hook should not be duplicated:\n%s", string(gotAgain))
	}
}

func TestUpdateMkinitcpioHooksAppendsWithoutFilesystems(t *testing.T) {
	input := []byte("HOOKS=(base udev)\n")
	got, changed := updateMkinitcpioHooks(input, true)
	if !changed {
		t.Fatal("expected mkinitcpio config to change")
	}
	if !strings.Contains(string(got), "HOOKS=(base udev bootrecov)") {
		t.Fatalf("unexpected mkinitcpio config:\n%s", string(got))
	}
}

func TestUpdateMkinitcpioHooksRemovesOnlyBootrecov(t *testing.T) {
	input := []byte("HOOKS=(base filesystems bootrecov keyboard)\n")
	got, changed := updateMkinitcpioHooks(input, false)
	if !changed {
		t.Fatal("expected mkinitcpio config to change")
	}
	if string(got) != "HOOKS=(base filesystems keyboard)\n" {
		t.Fatalf("unexpected mkinitcpio config:\n%s", string(got))
	}
}

func TestRenderMkinitcpioRuntimeHookRestoresModulesOnFallbackBoot(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	RootModulesDir = "/custom/modules"
	version := "6.6.7-arch1-1"
	newRoot := filepath.Join(t.TempDir(), "new-root")
	archive := filepath.Join(newRoot, "var", "backups", "bootrecov-snapshots", "snap", ".bootrecov", "root-modules", version+".sqfs")
	writeFile(t, archive)
	scriptPath := filepath.Join(t.TempDir(), "run-hook.sh")
	script := fmt.Sprintf(`#!/bin/sh
set -eu
cat() {
  if [ "${1:-}" = "/proc/cmdline" ]; then
    printf 'BOOT_IMAGE=/bootrecov-snapshots/snap/vmlinuz-linux bootrecov_entry=snap\n'
    return 0
  fi
  command cat "$@"
}
uname() {
  if [ "${1:-}" = "-r" ]; then
    printf '%%s\n' %s
    return 0
  fi
  command uname "$@"
}
unsquashfs() {
  if [ "${1:-}" != "-d" ]; then
    return 2
  fi
  mkdir -p "$2"
  printf 'restored\n' >"$2/modules.dep"
}
newroot=%s
%s
run_latehook
test -f "$newroot/custom/modules/%s/modules.dep"
`, shellSingleQuote(version), shellSingleQuote(newRoot), renderMkinitcpioRuntimeHook(), version)
	writeExecutable(t, scriptPath, script)

	out, err := exec.Command("sh", scriptPath).CombinedOutput()
	if err != nil {
		t.Fatalf("runtime hook did not restore modules: %v: %s", err, strings.TrimSpace(string(out)))
	}
}

func TestRenderMkinitcpioRuntimeHookHasShellSyntax(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bootrecov-hook")
	if err := os.WriteFile(path, []byte(renderMkinitcpioRuntimeHook()), 0o644); err != nil {
		t.Fatal(err)
	}
	checked := false
	for _, shell := range []string{"bash", "sh"} {
		if _, err := exec.LookPath(shell); err != nil {
			continue
		}
		checked = true
		if out, err := exec.Command(shell, "-n", path).CombinedOutput(); err != nil {
			t.Fatalf("runtime hook shell syntax failed with %s: %v: %s", shell, err, strings.TrimSpace(string(out)))
		}
	}
	if !checked {
		t.Skip("no shell available for syntax check")
	}
}

func TestCreateSquashFSModuleImageUsesAllRoot(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	src := filepath.Join(boot, "modules")
	dst := filepath.Join(efi, "modules.sqfs")
	writeFile(t, filepath.Join(src, "modules.dep"))
	argLog := filepath.Join(t.TempDir(), "mksquashfs.args")
	MksquashfsBin = filepath.Join(t.TempDir(), "mksquashfs")
	writeExecutable(t, MksquashfsBin, fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" >%s\n: >\"$2\"\n", shellSingleQuote(argLog)))

	if err := createSquashFSModuleImage(src, dst); err != nil {
		t.Fatalf("createSquashFSModuleImage failed: %v", err)
	}
	args, err := os.ReadFile(argLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "-all-root\n") {
		t.Fatalf("expected mksquashfs to receive -all-root, args:\n%s", string(args))
	}
}

func TestChownTreeToRootSetsRestoredModuleOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root privileges are required to verify restored module ownership")
	}
	root := filepath.Join(t.TempDir(), "modules", "6.6.7-arch1-1")
	moduleFile := filepath.Join(root, "modules.dep")
	writeFile(t, moduleFile)
	if err := os.Chown(root, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(moduleFile, 65534, 65534); err != nil {
		t.Fatal(err)
	}

	if err := chownTreeToRoot(root); err != nil {
		t.Fatalf("chownTreeToRoot failed: %v", err)
	}
	for _, path := range []string{root, moduleFile} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatalf("expected syscall.Stat_t for %s", path)
		}
		if stat.Uid != 0 || stat.Gid != 0 {
			t.Fatalf("expected %s to be owned by root, got uid=%d gid=%d", path, stat.Uid, stat.Gid)
		}
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

func TestInstallPacmanHookRejectsArchWithoutMkinitcpioConfig(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	if err := os.Remove(MkinitcpioConfPath); err != nil {
		t.Fatal(err)
	}

	err := InstallPacmanHook("/usr/bin/bootrecov")
	if !errors.Is(err, ErrUnsupportedInitramfsHook) {
		t.Fatalf("expected unsupported initramfs hook error, got %v", err)
	}
}

func TestInstallPacmanHookRejectsMkinitcpioConfigWithoutHooksLine(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFileWithContent(t, MkinitcpioConfPath, "MODULES=()\n")

	err := InstallPacmanHook("/usr/bin/bootrecov")
	if !errors.Is(err, ErrUnsupportedInitramfsHook) {
		t.Fatalf("expected unsupported initramfs hook error, got %v", err)
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
  -M, --metadata  Preserve metadata
      --times     Preserve time
`
	if !helpMentionsFlag(help, "--links") {
		t.Fatal("expected --links to be detected")
	}
	if !helpMentionsFlag(help, "--metadata") {
		t.Fatal("expected --metadata to be detected")
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
		"--metadata":      true,
		"--times":         true,
		"--delete-during": true,
		"--perms":         false,
	}
	args := buildRcloneSyncArgs("/src/", "/dst/", []string{"efi/boot-backups/**"}, nil, supported)
	got := strings.Join(args, " ")
	if strings.Contains(got, "--perms") {
		t.Fatalf("unexpected --perms in args: %q", got)
	}
	for _, want := range []string{"sync", "/src/", "/dst/", "--links", "--metadata", "--times", "--delete-during", "--exclude efi/boot-backups/**"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in args: %q", want, got)
		}
	}
}

func TestBuildRcloneSyncArgsWithIncludesAddsCatchAllExclude(t *testing.T) {
	supported := map[string]bool{
		"--links":         true,
		"--metadata":      true,
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

func TestArchivedRootModulesAreRestoreReadyButNotBootReady(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	version := "6.6.7-arch1-1"
	makeVersionedBootableBackup(t, snap, "old", version)
	makeVersionedBootableBackup(t, efi, "old", version)
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "old"), version))

	backups, err := DiscoverBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(backups))
	}
	b := backups[0]
	if IsBootReady(b) {
		t.Fatalf("archived modules should still require restore before boot-ready: %#v", b)
	}
	if !IsRestoreReady(b) {
		t.Fatalf("archived modules should be restorable: %#v", b)
	}
}

func TestStatusStringShowsRestoreForArchivedModules(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	version := "6.6.7-arch1-1"
	makeVersionedBootableBackup(t, snap, "old", version)
	makeVersionedBootableBackup(t, efi, "old", version)
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "old"), version))

	backups, err := DiscoverBackups()
	if err != nil {
		t.Fatal(err)
	}
	if got := statusString(backups[0]); got != "Restore" {
		t.Fatalf("statusString=%q want Restore for archived modules", got)
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

func TestActivateBackupRestoresArchivedRootModules(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	version := "6.6.7-arch1-1"
	makeVersionedBootableBackup(t, snap, "old", version)
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "old"), version))

	err := ActivateBackup("old")
	if err != nil {
		t.Fatalf("ActivateBackup failed: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(RootModulesDir, version, "modules.dep")); statErr != nil {
		t.Fatalf("activation should restore archived root module tree, err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(efi, "old")); statErr != nil {
		t.Fatalf("activation should create EFI mirror after module restore, err=%v", statErr)
	}
}

func TestAddGrubEntryRestoresArchivedRootModules(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	version := "6.6.7-arch1-1"
	makeVersionedBootableBackup(t, snap, "old", version)
	makeVersionedBootableBackup(t, efi, "old", version)
	writeFile(t, archivedModuleImagePath(filepath.Join(snap, "old"), version))

	if err := AddGrubEntry(BootBackup{Name: "old"}); err != nil {
		t.Fatalf("AddGrubEntry failed: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(RootModulesDir, version, "modules.dep")); statErr != nil {
		t.Fatalf("AddGrubEntry should restore archived root module tree, err=%v", statErr)
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
	if m2, _ := m.Update(keyPress("d")); m2 != nil {
		m = m2.(Model)
	}
	if !m.confirmDelete || m.deleteTarget != "keep" {
		t.Fatalf("expected delete confirmation state, got %#v", m)
	}
	if m2, _ := m.Update(keyPress("n")); m2 != nil {
		m = m2.(Model)
	}
	if m.confirmDelete {
		t.Fatal("expected delete confirmation to be canceled")
	}
	if _, err := os.Stat(filepath.Join(snap, "keep")); err != nil {
		t.Fatalf("backup should still exist after cancel: %v", err)
	}
	if m2, _ := m.Update(keyPress("d")); m2 != nil {
		m = m2.(Model)
	}
	if m2, cmd := m.Update(keyPress("y")); m2 != nil {
		m = m2.(Model)
		if !m.busy || m.task != taskDelete || !strings.Contains(m.viewString(), "deleting keep") {
			t.Fatalf("expected delete progress state, got busy=%v task=%v view:\n%s", m.busy, m.task, m.viewString())
		}
		m = drainModelCmd(t, m, cmd)
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
