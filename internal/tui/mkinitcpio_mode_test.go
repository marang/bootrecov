package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPacmanHookRejectsSystemdInitramfsBeforeWrites(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFileWithContent(t, MkinitcpioConfPath, "HOOKS=(base systemd autodetect block filesystems)\n")

	err := InstallPacmanHook("/usr/bin/bootrecov")
	if !errors.Is(err, ErrUnsupportedInitramfsHook) || !strings.Contains(err.Error(), "systemd") {
		t.Fatalf("expected explicit systemd initramfs refusal, got %v", err)
	}
	for _, path := range []string{PacmanHookPath, PacmanPostHookPath, MkinitcpioInstallPath, MkinitcpioHookPath} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("unsupported initramfs wrote %s: %v", path, statErr)
		}
	}
}

func TestHookInstalledDoesNotClaimSystemdRuntimeRestore(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	if err := InstallPacmanHook("/usr/bin/bootrecov"); err != nil {
		t.Fatal(err)
	}
	if !HookInstalled() {
		t.Fatal("busybox runtime hook should be detected after installation")
	}
	writeFileWithContent(t, MkinitcpioConfPath, "HOOKS=(base systemd block filesystems bootrecov)\n")
	if HookInstalled() {
		t.Fatal("installed files and HOOKS token must not imply systemd runtime restore")
	}
}

func TestInstallPacmanHookRejectsSystemdDropIn(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFileWithContent(t, MkinitcpioConfPath+".d/20-init.conf", "HOOKS=(base systemd block filesystems)\n")

	err := InstallPacmanHook("/usr/bin/bootrecov")
	if !errors.Is(err, ErrUnsupportedInitramfsHook) || !strings.Contains(err.Error(), "systemd") {
		t.Fatalf("expected effective systemd drop-in refusal, got %v", err)
	}
	if _, statErr := os.Stat(PacmanHookPath); !os.IsNotExist(statErr) {
		t.Fatalf("unsupported drop-in wrote package hook: %v", statErr)
	}
}

func TestInstallPacmanHookRejectsBusyboxDropInOverride(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFileWithContent(t, MkinitcpioConfPath+".d/20-init.conf", "HOOKS=(base udev block filesystems)\n")
	if err := InstallPacmanHook("/usr/bin/bootrecov"); !errors.Is(err, ErrUnsupportedInitramfsHook) {
		t.Fatalf("main config hook would be overridden by drop-in, got %v", err)
	}
}

func TestHookInstalledRejectsLaterDropInOverride(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	if err := InstallPacmanHook("/usr/bin/bootrecov"); err != nil {
		t.Fatal(err)
	}
	writeFileWithContent(t, MkinitcpioConfPath+".d/20-init.conf", "HOOKS=(base udev block filesystems)\n")
	if HookInstalled() {
		t.Fatal("drop-in removes the effective Bootrecov runtime hook")
	}
}

func TestInstallPacmanHookRejectsPresetUsingOtherConfig(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	customConf := filepath.Join(filepath.Dir(MkinitcpioConfPath), "other.conf")
	writeFileWithContent(t, customConf, "HOOKS=(base systemd block filesystems)\n")
	writeFileWithContent(t, filepath.Join(filepath.Dir(MkinitcpioConfPath), "mkinitcpio.d", "linux.preset"),
		"PRESETS=('default' 'fallback')\nALL_config='"+customConf+"'\n")

	err := InstallPacmanHook("/usr/bin/bootrecov")
	if !errors.Is(err, ErrUnsupportedInitramfsHook) || !strings.Contains(err.Error(), "preset") {
		t.Fatalf("expected custom preset config refusal, got %v", err)
	}
	if _, statErr := os.Stat(PacmanHookPath); !os.IsNotExist(statErr) {
		t.Fatalf("unsupported preset wrote package hook: %v", statErr)
	}
}

func TestInstallPacmanHookAcceptsBusyboxPresetUsingMainConfig(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFileWithContent(t, filepath.Join(filepath.Dir(MkinitcpioConfPath), "mkinitcpio.d", "linux.preset"),
		"PRESETS=('default' 'fallback')\nALL_config='"+MkinitcpioConfPath+"'\n")
	if err := InstallPacmanHook("/usr/bin/bootrecov"); err != nil {
		t.Fatalf("standard busybox preset should remain supported: %v", err)
	}
	if !HookInstalled() {
		t.Fatal("installed busybox hook should be reported as installed")
	}
}

func TestInstallPacmanHookRejectsPresetHookOptions(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFileWithContent(t, filepath.Join(filepath.Dir(MkinitcpioConfPath), "mkinitcpio.d", "linux.preset"),
		"PRESETS=('default')\ndefault_options='-S bootrecov'\n")
	if err := InstallPacmanHook("/usr/bin/bootrecov"); !errors.Is(err, ErrUnsupportedInitramfsHook) {
		t.Fatalf("preset can remove runtime hook from image: %v", err)
	}
}

func TestInstallPacmanHookRejectsDynamicHooks(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFileWithContent(t, MkinitcpioConfPath, "HOOKS=(base udev block filesystems)\nsource /etc/other-hooks.conf\n")
	if err := InstallPacmanHook("/usr/bin/bootrecov"); !errors.Is(err, ErrUnsupportedInitramfsHook) {
		t.Fatalf("dynamically sourced hooks cannot be verified: %v", err)
	}
}

func TestInstallPacmanHookRejectsDeclaredSystemdHooks(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFileWithContent(t, MkinitcpioConfPath, "HOOKS=(base udev block filesystems)\ndeclare -a HOOKS=(base systemd block filesystems)\n")
	if err := InstallPacmanHook("/usr/bin/bootrecov"); !errors.Is(err, ErrUnsupportedInitramfsHook) {
		t.Fatalf("later declare must not make systemd look like BusyBox: %v", err)
	}
	if _, err := os.Stat(PacmanHookPath); !os.IsNotExist(err) {
		t.Fatalf("unsupported config wrote package hook: %v", err)
	}
}

func TestInstallPacmanHookIgnoresDropInForExplicitPresetConfig(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFileWithContent(t, MkinitcpioConfPath+".d/20-init.conf", "HOOKS=(base systemd block filesystems)\n")
	writeFileWithContent(t, filepath.Join(filepath.Dir(MkinitcpioConfPath), "mkinitcpio.d", "linux.preset"),
		"PRESETS=('default' 'fallback')\nALL_config='"+MkinitcpioConfPath+"'\n")
	if err := InstallPacmanHook("/usr/bin/bootrecov"); err != nil {
		t.Fatalf("explicit -c preset ignores drop-ins and uses BusyBox: %v", err)
	}
	if !HookInstalled() {
		t.Fatal("BusyBox preset with explicit main config should be installed")
	}
}

func TestInstallPacmanHookRejectsMixedPresetWithSystemdDropIn(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFileWithContent(t, MkinitcpioConfPath+".d/20-init.conf", "HOOKS=(base systemd block filesystems)\n")
	writeFileWithContent(t, filepath.Join(filepath.Dir(MkinitcpioConfPath), "mkinitcpio.d", "linux.preset"),
		"PRESETS=('default' 'fallback')\ndefault_config='"+MkinitcpioConfPath+"'\n")
	if err := InstallPacmanHook("/usr/bin/bootrecov"); !errors.Is(err, ErrUnsupportedInitramfsHook) {
		t.Fatalf("fallback still sources systemd drop-in: %v", err)
	}
}

func TestCurrentRuntimeEnvironmentReportsSystemdMode(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	writeFileWithContent(t, MkinitcpioConfPath, "HOOKS=(base systemd block filesystems)\n")
	info := CurrentRuntimeEnvironment()
	if info.InitramfsMode != "systemd" || !strings.Contains(info.InitramfsReason, "systemd") {
		t.Fatalf("doctor data must explain unsupported systemd initramfs: %#v", info)
	}
}

func TestInstallPacmanHookRegeneratesInspectedConfigDespiteMkinitcpioEnv(t *testing.T) {
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	otherConf := filepath.Join(t.TempDir(), "systemd.conf")
	writeFileWithContent(t, otherConf, "HOOKS=(base systemd block filesystems)\n")
	t.Setenv("MKINITCPIO_CONF", otherConf)
	UpdateInitramfs = true
	output := filepath.Join(t.TempDir(), "used-config")
	MkinitcpioBin = filepath.Join(t.TempDir(), "mkinitcpio")
	writeExecutable(t, MkinitcpioBin, fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$MKINITCPIO_CONF\" > %q\n", output))
	if err := InstallPacmanHook("/usr/bin/bootrecov"); err != nil {
		t.Fatal(err)
	}
	used, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(used) != MkinitcpioConfPath {
		t.Fatalf("mkinitcpio used %q, inspected %q", used, MkinitcpioConfPath)
	}
}
