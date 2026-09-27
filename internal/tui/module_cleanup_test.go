package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setupModuleCleanupTest(t *testing.T) func(string, ...string) ([]byte, error) {
	t.Helper()
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	if err := os.MkdirAll(RootModulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mountInfoPath = filepath.Join(filepath.Dir(grub), "mountinfo")
	writeFileWithContent(t, mountInfoPath, "1 0 0:1 / / rw - ext4 root rw\n")
	oldCommand, oldLookPath, oldBusy, oldHold := moduleCleanupCommand, moduleCleanupLookPath, moduleCleanupPackageBusy, moduleCleanupHoldPackageLock
	t.Cleanup(func() {
		moduleCleanupCommand, moduleCleanupLookPath, moduleCleanupPackageBusy, moduleCleanupHoldPackageLock = oldCommand, oldLookPath, oldBusy, oldHold
	})
	moduleCleanupLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	moduleCleanupPackageBusy = func(string) (bool, error) { return false, nil }
	moduleCleanupHoldPackageLock = func(string) (func() error, bool, error) { return func() error { return nil }, false, nil }
	moduleCleanupCommand = func(name string, args ...string) ([]byte, error) {
		switch name {
		case "uname":
			return []byte("6.99.0-running\n"), nil
		case "pacman", "rpm":
			return []byte("/usr/bin/placeholder\n"), nil
		case "file":
			return []byte("Linux kernel x86 boot executable, bzImage, version 6.1.0-old (test)\n"), nil
		default:
			return nil, errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
		}
	}
	return moduleCleanupCommand
}

func makeMarkedModules(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(RootModulesDir, version)
	writeFileWithContent(t, filepath.Join(path, "kernel", "module.ko"), "module")
	if err := markRestoredModuleTree(path, version); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRestoredModuleMarkerBindsVersionAndInode(t *testing.T) {
	setupModuleCleanupTest(t)
	path := makeMarkedModules(t, "6.1.0-old")
	original, err := os.ReadFile(filepath.Join(path, restoredModuleMarker))
	if err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(RootModulesDir, ".staging")
	if err := os.Rename(path, staging); err != nil {
		t.Fatal(err)
	}
	if owned, err := ownedRestoredModuleTree(staging); err != nil || owned {
		t.Fatalf("renamed owned=%v err=%v", owned, err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileWithContent(t, filepath.Join(path, restoredModuleMarker), string(original))
	if owned, err := ownedRestoredModuleTree(path); err != nil || owned {
		t.Fatalf("copied marker owned=%v err=%v", owned, err)
	}
	if err := markRestoredModuleTree(staging, "6.2.0-next"); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(RootModulesDir, "6.2.0-next")
	if err := os.Rename(staging, final); err != nil {
		t.Fatal(err)
	}
	if owned, err := ownedRestoredModuleTree(final); err != nil || !owned {
		t.Fatalf("staging rename owned=%v err=%v", owned, err)
	}
}

func TestRestoredModuleMarkerDoesNotFollowArchiveSymlink(t *testing.T) {
	setupModuleCleanupTest(t)
	path := makeMarkedModules(t, "6.1.0-old")
	marker := filepath.Join(path, restoredModuleMarker)
	outside := filepath.Join(t.TempDir(), "outside")
	writeFileWithContent(t, outside, "untouched")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, marker); err != nil {
		t.Fatal(err)
	}
	if owned, err := ownedRestoredModuleTree(path); err != nil || owned {
		t.Fatalf("symlink marker owned=%v err=%v", owned, err)
	}
	if err := markRestoredModuleTree(path, "6.1.0-old"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "untouched" {
		t.Fatalf("outside changed: %q %v", data, err)
	}
}

func TestCleanupRestoredModuleTreesDeletesOnlyOwnedUnused(t *testing.T) {
	setupModuleCleanupTest(t)
	unused := makeMarkedModules(t, "6.1.0-old")
	running := makeMarkedModules(t, "6.99.0-running")
	unmarked := filepath.Join(RootModulesDir, "6.2.0-unmarked")
	writeFileWithContent(t, filepath.Join(unmarked, "modules.dep"), "keep")
	outside := t.TempDir()
	symlink := filepath.Join(RootModulesDir, "6.3.0-symlink")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Fatal(err)
	}
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unused); !os.IsNotExist(err) {
		t.Fatalf("unused survived: %v", err)
	}
	for _, path := range []string{running, unmarked, symlink, outside} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("protected %s: %v", path, err)
		}
	}
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupRestoredModuleTreesUnmarkedNeedsNoTools(t *testing.T) {
	setupModuleCleanupTest(t)
	writeFileWithContent(t, filepath.Join(RootModulesDir, "6.1.0", "modules.dep"), "keep")
	moduleCleanupCommand = func(string, ...string) ([]byte, error) { t.Fatal("unexpected external command"); return nil, nil }
	moduleCleanupPackageBusy = func(string) (bool, error) { t.Fatal("unexpected lock query"); return false, nil }
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
}

func TestArchCleanupHoldsConfiguredPacmanLock(t *testing.T) {
	setupModuleCleanupTest(t)
	oldDBPath := PacmanDBPath
	PacmanDBPath = t.TempDir()
	t.Cleanup(func() { PacmanDBPath = oldDBPath })
	release, busy, err := holdCleanupPackageLock(PlatformArch)
	if err != nil || busy {
		t.Fatalf("first lock: busy=%v err=%v", busy, err)
	}
	_, busy, err = holdCleanupPackageLock(PlatformArch)
	if err != nil || !busy {
		t.Fatalf("second lock: busy=%v err=%v", busy, err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	release, busy, err = holdCleanupPackageLock(PlatformArch)
	if err != nil || busy {
		t.Fatalf("lock after release: busy=%v err=%v", busy, err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}

func TestArchCleanupUsesPacmanConfiguredDBPath(t *testing.T) {
	setupModuleCleanupTest(t)
	oldDBPath := PacmanDBPath
	PacmanDBPath = ""
	t.Cleanup(func() { PacmanDBPath = oldDBPath })
	dbPath := t.TempDir()
	moduleCleanupCommand = func(name string, args ...string) ([]byte, error) {
		if name == "pacman-conf" && len(args) == 1 && args[0] == "DBPath" {
			return []byte(dbPath + "/\n"), nil
		}
		return nil, errors.New("unexpected command")
	}
	release, busy, err := holdCleanupPackageLock(PlatformArch)
	if err != nil || busy {
		t.Fatalf("configured lock: busy=%v err=%v", busy, err)
	}
	if _, err := os.Stat(filepath.Join(dbPath, "db.lck")); err != nil {
		t.Fatalf("configured database was not locked: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupRestoredModuleTreesSafetyGuards(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(*testing.T, string)
		wantError bool
	}{
		{"package owns nested file", func(t *testing.T, path string) {
			base := moduleCleanupCommand
			moduleCleanupCommand = func(n string, a ...string) ([]byte, error) {
				if n == "pacman" {
					return []byte(path + "/kernel/not-currently-on-disk.ko\n"), nil
				}
				return base(n, a...)
			}
		}, false},
		{"Fedora retains marked tree until package lock support", func(t *testing.T, path string) {
			activePlatformID = PlatformFedora
		}, false},
		{"package query fails", func(t *testing.T, path string) {
			base := moduleCleanupCommand
			moduleCleanupCommand = func(n string, a ...string) ([]byte, error) {
				if n == "pacman" {
					return nil, errors.New("database unavailable")
				}
				return base(n, a...)
			}
		}, true},
		{"package output malformed", func(t *testing.T, path string) {
			base := moduleCleanupCommand
			moduleCleanupCommand = func(n string, a ...string) ([]byte, error) {
				if n == "pacman" {
					return []byte("warning: database unavailable\n"), nil
				}
				return base(n, a...)
			}
		}, true},
		{"transaction lock", func(t *testing.T, path string) {
			moduleCleanupPackageBusy = func(string) (bool, error) { return true, nil }
		}, false},
		{"running unknown", func(t *testing.T, path string) {
			base := moduleCleanupCommand
			moduleCleanupCommand = func(n string, a ...string) ([]byte, error) {
				if n == "uname" {
					return nil, errors.New("uname failure")
				}
				return base(n, a...)
			}
		}, true},
		{"nested mount", func(t *testing.T, path string) {
			writeFileWithContent(t, mountInfoPath, "1 0 0:1 / / rw - ext4 root rw\n2 1 0:2 / "+path+"/kernel rw - tmpfs none rw\n")
		}, true},
		{"tree mount", func(t *testing.T, path string) {
			writeFileWithContent(t, mountInfoPath, "1 0 0:1 / / rw - ext4 root rw\n2 1 0:2 / "+path+" rw - tmpfs none rw\n")
		}, true},
		{"mount table malformed", func(t *testing.T, path string) { writeFileWithContent(t, mountInfoPath, "garbage\n") }, true},
		{"primary versioned kernel", func(t *testing.T, path string) {
			writeFileWithContent(t, filepath.Join(BootDir, "vmlinuz-6.1.0-old"), "kernel")
		}, false},
		{"primary filename disagrees with booted kernel", func(t *testing.T, path string) {
			writeFileWithContent(t, filepath.Join(BootDir, "vmlinuz-6.2.0-new"), "kernel")
		}, false},
		{"symlinked boot directory", func(t *testing.T, path string) {
			realBoot := filepath.Join(t.TempDir(), "real-boot")
			if err := os.Mkdir(realBoot, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(BootDir); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(realBoot, BootDir); err != nil {
				t.Fatal(err)
			}
			writeFileWithContent(t, filepath.Join(realBoot, "vmlinuz-6.1.0-old"), "kernel")
		}, false},
		{"primary unknown kernel", func(t *testing.T, path string) {
			writeFileWithContent(t, filepath.Join(BootDir, "vmlinuz-custom"), "kernel")
			base := moduleCleanupCommand
			moduleCleanupCommand = func(n string, a ...string) ([]byte, error) {
				if n == "file" {
					return []byte("data\n"), nil
				}
				return base(n, a...)
			}
		}, true},
		{"primary boot reference", func(t *testing.T, path string) {
			writeFileWithContent(t, GrubCfgOutput, "linux /special/vmlinuz-6.1.0-old root=UUID=test\n")
		}, false},
		{"unknown active entry", func(t *testing.T, path string) {
			writeFileWithContent(t, GrubCustom, "menuentry 'Bootrecov /missing/recovery' --id bootrecov-recovery {\n}\n")
		}, true},
		{"malformed active BLS", func(t *testing.T, path string) {
			writeFileWithContent(t, filepath.Join(BLSEntriesDir, "bootrecov-bad.conf"), "title invalid\n")
		}, true},
		{"inactive unarchived snapshot", func(t *testing.T, path string) {
			writeFileWithContent(t, filepath.Join(SnapshotDir, "kept", "vmlinuz-6.1.0-old"), "kernel")
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupModuleCleanupTest(t)
			path := makeMarkedModules(t, "6.1.0-old")
			tc.setup(t, path)
			err := cleanupRestoredModuleTrees()
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v wantError=%v", err, tc.wantError)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("protected tree removed: %v", err)
			}
		})
	}
}

func TestCleanupRestoredModuleTreesArchivedInactiveSnapshotCanRestoreAgain(t *testing.T) {
	setupModuleCleanupTest(t)
	path := makeMarkedModules(t, "6.1.0-old")
	snapshot := filepath.Join(SnapshotDir, "inactive")
	writeFileWithContent(t, filepath.Join(snapshot, "vmlinuz-6.1.0-old"), "kernel")
	writeFileWithContent(t, archivedModuleImagePath(snapshot, "6.1.0-old"), "archive")
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unused modules retained: %v", err)
	}
}

func TestCleanupRestoredDKMSOnlyCandidateKernel(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "remove failure"}[failure], func(t *testing.T) {
			base := setupModuleCleanupTest(t)
			path := makeMarkedModules(t, "6.1.0-old")
			moduleCleanupLookPath = func(string) (string, error) { return "/fake/dkms", nil }
			var removals []string
			moduleCleanupCommand = func(n string, a ...string) ([]byte, error) {
				if n != "dkms" {
					return base(n, a...)
				}
				if len(a) == 1 && a[0] == "status" {
					return []byte("nvidia/550.1, 6.1.0-old, x86_64: installed\nnvidia/550.1, 6.99.0-running, x86_64: installed\nzfs/2.2.0: added\n"), nil
				}
				removals = append(removals, strings.Join(a, " "))
				if failure {
					return nil, errors.New("remove failed")
				}
				return nil, nil
			}
			err := cleanupRestoredModuleTrees()
			if (err != nil) != failure {
				t.Fatalf("err=%v", err)
			}
			if len(removals) != 1 || removals[0] != "remove -m nvidia -v 550.1 -k 6.1.0-old" {
				t.Fatalf("unsafe removals: %v", removals)
			}
			_, statErr := os.Stat(path)
			if failure && statErr != nil {
				t.Fatal("failed DKMS removal deleted tree")
			}
			if !failure && !os.IsNotExist(statErr) {
				t.Fatal("successful cleanup retained tree")
			}
		})
	}
}

func TestCleanupRestoredDKMSMalformedStatusPreventsAnyRemoval(t *testing.T) {
	base := setupModuleCleanupTest(t)
	path := makeMarkedModules(t, "6.1.0-old")
	moduleCleanupLookPath = func(string) (string, error) { return "/fake/dkms", nil }
	moduleCleanupCommand = func(n string, a ...string) ([]byte, error) {
		if n != "dkms" {
			return base(n, a...)
		}
		if strings.Join(a, " ") != "status" {
			t.Fatal("mutated DKMS before full parse")
		}
		return []byte("nvidia/550.1, 6.1.0-old, x86_64: installed\nnot understood\n"), nil
	}
	if err := cleanupRestoredModuleTrees(); err == nil {
		t.Fatal("wanted parse failure")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("tree removed")
	}
}

func TestCleanupRestoredModulesActiveArchivedSnapshotStillProtected(t *testing.T) {
	setupModuleCleanupTest(t)
	path := makeMarkedModules(t, "6.1.0-old")
	snapshot := filepath.Join(SnapshotDir, "active")
	writeFileWithContent(t, filepath.Join(snapshot, "vmlinuz-6.1.0-old"), "kernel")
	writeFileWithContent(t, archivedModuleImagePath(snapshot, "6.1.0-old"), "archive")
	writeFileWithContent(t, GrubCustom, "menuentry 'Bootrecov "+filepath.Join(EfiDir, "active")+"' --id bootrecov-active {\n}\n")
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("active modules removed")
	}
}

func TestCleanupRestoredModulesPrimaryUnversionedImage(t *testing.T) {
	base := setupModuleCleanupTest(t)
	path := makeMarkedModules(t, "6.1.0-old")
	writeFileWithContent(t, filepath.Join(BootDir, "vmlinuz-custom"), "kernel")
	moduleCleanupCommand = func(n string, a ...string) ([]byte, error) {
		if n == "file" {
			return []byte("Linux kernel x86 boot executable bzImage, version 6.1.0-old (builder)\n"), nil
		}
		return base(n, a...)
	}
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("primary modules removed")
	}
}

func TestCleanupRestoredModulesDKMSQueryFailure(t *testing.T) {
	base := setupModuleCleanupTest(t)
	path := makeMarkedModules(t, "6.1.0-old")
	moduleCleanupLookPath = func(string) (string, error) { return "/fake/dkms", nil }
	moduleCleanupCommand = func(n string, a ...string) ([]byte, error) {
		if n == "dkms" {
			return nil, errors.New("DKMS database failed")
		}
		return base(n, a...)
	}
	if err := cleanupRestoredModuleTrees(); err == nil {
		t.Fatal("wanted DKMS query error")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("tree removed after failed query")
	}
}

func TestCleanupRestoredModulesPackageAlias(t *testing.T) {
	base := setupModuleCleanupTest(t)
	path := makeMarkedModules(t, "6.1.0-old")
	alias := filepath.Join(filepath.Dir(RootModulesDir), "modules-alias")
	if err := os.Symlink(RootModulesDir, alias); err != nil {
		t.Fatal(err)
	}
	RootModulesDir = alias
	moduleCleanupCommand = func(n string, a ...string) ([]byte, error) {
		if n == "pacman" {
			return []byte(alias + "/6.1.0-old/kernel/module.ko\n"), nil
		}
		return base(n, a...)
	}
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("package owned alias removed")
	}
}

func TestCleanupProtectsOtherBootEntryUsingRecoveryMirror(t *testing.T) {
	cases := []struct {
		name, reference string
		bls             bool
	}{
		{"grub-visible", "/bootrecov-snapshots/old/vmlinuz-linux", false},
		{"grub-root", "($root)/bootrecov-snapshots/old/vmlinuz-linux", false},
		{"host-path", "host-path", false},
		{"bls", "/bootrecov-snapshots/old/vmlinuz-linux", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupModuleCleanupTest(t)
			EfiDir = filepath.Join(EfiDir, "bootrecov-snapshots")
			old := makeMarkedModules(t, "6.1.0-old")
			unused := makeMarkedModules(t, "6.2.0-unused")
			mirrorImage := filepath.Join(EfiDir, "old", "vmlinuz-linux")
			writeFileWithContent(t, mirrorImage, "kernel")
			kernelRef := tc.reference
			if tc.name == "host-path" {
				kernelRef = mirrorImage
			}
			if tc.bls {
				writeFileWithContent(t, filepath.Join(BLSEntriesDir, "manual.conf"), "title Manual fallback\nlinux "+kernelRef+"\n")
			} else {
				writeFileWithContent(t, GrubCfgOutput, "menuentry 'Manual fallback' {\n  linux "+kernelRef+" root=UUID=test\n}\n")
			}
			id := backupIDForName("old")
			writeFileWithContent(t, GrubCustom, "cat <<'EOF'\nmenuentry 'Bootrecov "+filepath.Join(EfiDir, "old")+"' --id "+id+" {\n}\nEOF\n")
			if err := RemoveGrubEntry(id); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(old); err != nil {
				t.Fatalf("modules needed by manual entry were removed: %v", err)
			}
			if _, err := os.Stat(unused); !os.IsNotExist(err) {
				t.Fatalf("unrelated unused modules were retained: %v", err)
			}
		})
	}
}

func TestCleanupKeepsModulesWhenRecoveryMirrorReferenceCannotBeResolved(t *testing.T) {
	setupModuleCleanupTest(t)
	EfiDir = filepath.Join(EfiDir, "bootrecov-snapshots")
	path := makeMarkedModules(t, "6.1.0-old")
	writeFileWithContent(t, GrubCfgOutput, "linux /bootrecov-snapshots/missing/vmlinuz-linux root=UUID=test\n")
	if err := cleanupRestoredModuleTrees(); err == nil {
		t.Fatal("missing referenced recovery kernel should stop cleanup")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("modules removed despite unresolved recovery reference: %v", err)
	}
}

func TestCleanupProtectsKernelReferencedBySourcedGrubConfig(t *testing.T) {
	setupModuleCleanupTest(t)
	EfiDir = filepath.Join(EfiDir, "bootrecov-snapshots")
	old := makeMarkedModules(t, "6.1.0-old")
	unused := makeMarkedModules(t, "6.2.0-unused")
	writeFileWithContent(t, filepath.Join(EfiDir, "old", "vmlinuz-linux"), "kernel")
	configDir := filepath.Dir(GrubCfgOutput)
	writeFileWithContent(t, GrubCfgOutput, "if [ -f ${config_directory}/custom.cfg ]; then\n  source ${config_directory}/custom.cfg\nelif [ -z \"${config_directory}\" -a -f $prefix/custom.cfg ]; then\n  source $prefix/custom.cfg\nfi\n")
	writeFileWithContent(t, filepath.Join(configDir, "custom.cfg"), "menuentry 'Nested' {source ${config_directory}/nested.cfg;}\n")
	writeFileWithContent(t, filepath.Join(configDir, "nested.cfg"), "menuentry 'Manual fallback' { linux /bootrecov-snapshots/old/vmlinuz-linux root=UUID=test; }\n")
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("modules referenced by sourced config were removed: %v", err)
	}
	if _, err := os.Stat(unused); !os.IsNotExist(err) {
		t.Fatalf("unrelated unused modules were retained: %v", err)
	}
}

func TestCleanupResolvesGrubPrefixAcrossConfigfile(t *testing.T) {
	setupModuleCleanupTest(t)
	EfiDir = filepath.Join(EfiDir, "bootrecov-snapshots")
	old := makeMarkedModules(t, "6.1.0-old")
	unused := makeMarkedModules(t, "6.2.0-unused")
	writeFileWithContent(t, filepath.Join(EfiDir, "old", "vmlinuz-linux"), "kernel")
	configDir := filepath.Dir(GrubCfgOutput)
	other := filepath.Join(filepath.Dir(configDir), "extra", "main.cfg")
	writeFileWithContent(t, GrubCfgOutput, "configfile "+other+"\n")
	writeFileWithContent(t, other, "source $prefix/custom.cfg\n")
	writeFileWithContent(t, filepath.Join(configDir, "custom.cfg"), "linux /bootrecov-snapshots/old/vmlinuz-linux\n")
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("modules referenced through $prefix were removed: %v", err)
	}
	if _, err := os.Stat(unused); !os.IsNotExist(err) {
		t.Fatalf("unrelated unused modules were retained: %v", err)
	}
}

func TestCleanupSourceRetainsConfigDirectory(t *testing.T) {
	setupModuleCleanupTest(t)
	EfiDir = filepath.Join(EfiDir, "bootrecov-snapshots")
	old := makeMarkedModules(t, "6.1.0-old")
	unused := makeMarkedModules(t, "6.2.0-unused")
	writeFileWithContent(t, filepath.Join(EfiDir, "old", "vmlinuz-linux"), "kernel")
	configDir := filepath.Dir(GrubCfgOutput)
	writeFileWithContent(t, GrubCfgOutput, "source "+filepath.Join(configDir, "sub", "nested.cfg")+"\n")
	writeFileWithContent(t, filepath.Join(configDir, "sub", "nested.cfg"), "source ${config_directory}/custom.cfg\n")
	writeFileWithContent(t, filepath.Join(configDir, "custom.cfg"), "linux /bootrecov-snapshots/old/vmlinuz-linux\n")
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("modules referenced through inherited config_directory were removed: %v", err)
	}
	if _, err := os.Stat(unused); !os.IsNotExist(err) {
		t.Fatalf("unrelated unused modules were retained: %v", err)
	}
}

func TestCleanupFailsClosedWhenGrubPrefixChanges(t *testing.T) {
	setupModuleCleanupTest(t)
	path := makeMarkedModules(t, "6.1.0-old")
	otherPrefix := filepath.Join(filepath.Dir(filepath.Dir(GrubCfgOutput)), "other-grub")
	writeFileWithContent(t, GrubCfgOutput, "set prefix="+otherPrefix+"; source $prefix/custom.cfg\n")
	writeFileWithContent(t, filepath.Join(otherPrefix, "custom.cfg"), "linux /bootrecov-snapshots/old/vmlinuz-linux\n")
	if err := cleanupRestoredModuleTrees(); err == nil {
		t.Fatal("changed GRUB prefix should prevent cleanup")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("modules removed despite changed GRUB prefix: %v", err)
	}
}

func TestCleanupAllowsAbsentConditionalCustomConfig(t *testing.T) {
	setupModuleCleanupTest(t)
	path := makeMarkedModules(t, "6.1.0-old")
	writeFileWithContent(t, GrubCfgOutput, "if [ -f ${config_directory}/custom.cfg ]; then\n  source ${config_directory}/custom.cfg\nfi\n")
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unused modules survived absent optional config: %v", err)
	}
}

func TestCleanupFailsClosedOnUnknownGrubSource(t *testing.T) {
	setupModuleCleanupTest(t)
	path := makeMarkedModules(t, "6.1.0-old")
	writeFileWithContent(t, GrubCfgOutput, "source $external/other.cfg\n")
	if err := cleanupRestoredModuleTrees(); err == nil {
		t.Fatal("unknown source should prevent cleanup")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("modules removed despite unknown source: %v", err)
	}
}

func TestCleanupFailsClosedOnRelativeGrubSource(t *testing.T) {
	setupModuleCleanupTest(t)
	path := makeMarkedModules(t, "6.1.0-old")
	writeFileWithContent(t, GrubCfgOutput, "source nested.cfg\n")
	writeFileWithContent(t, filepath.Join(filepath.Dir(GrubCfgOutput), "nested.cfg"), "")
	if err := cleanupRestoredModuleTrees(); err == nil {
		t.Fatal("relative GRUB source should prevent cleanup")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("modules removed despite ambiguous relative source: %v", err)
	}
}

func TestActivationMarksRestoredModulesForFinalEntryCleanup(t *testing.T) {
	setupModuleCleanupTest(t)
	version := "6.1.0-old"
	makeVersionedBootableBackup(t, SnapshotDir, "old", version)
	writeFileWithContent(t, archivedModuleImagePath(filepath.Join(SnapshotDir, "old"), version), "archive")
	stub := filepath.Join(t.TempDir(), "unsquashfs-stub")
	writeFileWithContent(t, stub, "#!/bin/sh\n[ \"$1\" = -d ] || exit 2\nmkdir -p \"$2/kernel\"\nprintf 'restored\\n' > \"$2/modules.dep\"\n")
	if err := os.Chmod(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	UnsquashfsBin = stub
	restoreModuleTreeFunc = restoreSquashFSModuleTree
	oldChown := chownRestoredModuleTreeFunc
	chownRestoredModuleTreeFunc = func(string) error { return nil }
	t.Cleanup(func() { chownRestoredModuleTreeFunc = oldChown })
	if err := ActivateBackup("old"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(RootModulesDir, version)
	if owned, err := ownedRestoredModuleTree(path); err != nil || !owned {
		t.Fatalf("production restore did not mark module tree: owned=%v err=%v", owned, err)
	}
	if err := DeleteBackup("old"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("marked modules survived final entry removal: %v", err)
	}
}

// Give GRUB two distinct mounted devices with identically named kernel images.
// Identifying the host /boot image must not substitute for the selected device.
func setupCleanupGRUBDevices(t *testing.T) string {
	t.Helper()
	base := setupModuleCleanupTest(t)
	other := t.TempDir()
	writeFileWithContent(t, mountInfoPath, "1 0 0:1 / / rw - ext4 root rw\n2 1 0:2 / "+BootDir+" rw - ext4 boot rw\n3 1 0:3 / "+other+" rw - ext4 other rw\n")
	writeFileWithContent(t, filepath.Join(BootDir, "vmlinuz-linux"), "host kernel")
	writeFileWithContent(t, filepath.Join(other, "vmlinuz-linux"), "selected kernel")
	moduleCleanupCommand = func(name string, args ...string) ([]byte, error) {
		if name == "grub-probe" {
			path := args[len(args)-1]
			switch args[0] {
			case "--target=fs_uuid":
				if path == BootDir {
					return []byte("host-uuid\n"), nil
				}
				if path == other {
					return []byte("other-uuid\n"), nil
				}
			case "--target=drive":
				if path == BootDir {
					return []byte("(hd0,gpt1)\n"), nil
				}
				if path == other {
					return []byte("(hd1,gpt1)\n"), nil
				}
			}
			return nil, errors.New("unproved mount")
		}
		if name == "file" {
			if args[len(args)-1] == filepath.Join(BootDir, "vmlinuz-linux") {
				return []byte("Linux kernel version 6.3.0-host\n"), nil
			}
		}
		return base(name, args...)
	}
	return other
}

func TestCleanupGRUBProtectsSelectedFilesystemKernel(t *testing.T) {
	for _, selection := range []string{
		"search --no-floppy --fs-uuid --set=root other-uuid",
		"set root='hd1,gpt1'",
		"'search' --no-floppy --fs-uuid --set=root other-uuid",
	} {
		t.Run(selection, func(t *testing.T) {
			setupCleanupGRUBDevices(t)
			needed := makeMarkedModules(t, "6.1.0-old")
			unused := makeMarkedModules(t, "6.2.0-unused")
			writeFileWithContent(t, GrubCfgOutput, "menuentry 'Other' {\n"+selection+"\n'linux' ($root)/vmlinuz-linux root=UUID=other-root\n}\n")
			if err := cleanupRestoredModuleTrees(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(needed); err != nil {
				t.Fatalf("selected device kernel modules removed: %v", err)
			}
			if _, err := os.Stat(unused); !os.IsNotExist(err) {
				t.Fatalf("unused modules retained: %v", err)
			}
		})
	}
}

func TestCleanupGRUBAllowsGeneratedArchRootSelection(t *testing.T) {
	setupCleanupGRUBDevices(t)
	needed := makeMarkedModules(t, "6.3.0-host")
	unused := makeMarkedModules(t, "6.2.0-unused")
	writeFileWithContent(t, GrubCfgOutput, `menuentry 'Arch Linux' {
  set root='hd0,gpt1'
  if [ x$feature_platform_search_hint = xy ]; then
    search --no-floppy --fs-uuid --set=root --hint-bios=hd0,gpt1 --hint-efi=hd0,gpt1 --hint-baremetal=ahci0,gpt1 host-uuid
  else
    search --no-floppy --fs-uuid --set=root host-uuid
  fi
  linux /vmlinuz-linux root=UUID=root-uuid rw
}
`)
	base := moduleCleanupCommand
	probes := map[string]int{}
	moduleCleanupCommand = func(name string, args ...string) ([]byte, error) {
		if name == "grub-probe" {
			probes[strings.Join(args, " ")]++
		}
		return base(name, args...)
	}
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(needed); err != nil {
		t.Fatalf("primary kernel modules removed: %v", err)
	}
	if _, err := os.Stat(unused); !os.IsNotExist(err) {
		t.Fatalf("unused modules retained: %v", err)
	}
	for probe, calls := range probes {
		if calls > 1 {
			t.Fatalf("probe %s repeated %d times", probe, calls)
		}
	}
}

func TestCleanupGRUBRetainsOnUnprovedRoot(t *testing.T) {
	for _, script := range []string{
		"search --fs-uuid --set=root unmounted-uuid\nlinux /vmlinuz-linux\n",
		"set root='hd99,gpt1'\nlinux /vmlinuz-linux\n",
		"set root=$other\nlinux /vmlinuz-linux\n",
		"unset root\nlinux /vmlinuz-linux\n",
		"search --file --set=root /vmlinuz-linux\nlinux /vmlinuz-linux\n",
		"search --fs-uuid --set=root host-uuid\nlinux (hd99,gpt1)/vmlinuz-linux\n",
		"search --fs-uuid --set=root host-uuid\nlinux \\\n/vmlinuz-linux\n",
	} {
		t.Run(script, func(t *testing.T) {
			setupCleanupGRUBDevices(t)
			needed := makeMarkedModules(t, "6.1.0-old")
			writeFileWithContent(t, GrubCfgOutput, script)
			if err := cleanupRestoredModuleTrees(); err == nil {
				t.Fatal("unresolved root should prevent cleanup")
			}
			if _, err := os.Stat(needed); err != nil {
				t.Fatalf("modules removed despite unproved root: %v", err)
			}
		})
	}
}

func TestCleanupGRUBSourceUsesSelectedFilesystem(t *testing.T) {
	for _, command := range []string{"source", "configfile"} {
		t.Run(command, func(t *testing.T) {
			other := setupCleanupGRUBDevices(t)
			needed := makeMarkedModules(t, "6.1.0-old")
			unused := makeMarkedModules(t, "6.2.0-unused")
			writeFileWithContent(t, filepath.Join(BootDir, "nested.cfg"), "linux /vmlinuz-linux\n")
			writeFileWithContent(t, filepath.Join(other, "nested.cfg"), "linux ($root)/vmlinuz-linux\n")
			writeFileWithContent(t, GrubCfgOutput, "search --fs-uuid --set=root other-uuid; "+command+" ($root)/nested.cfg\n")
			if err := cleanupRestoredModuleTrees(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(needed); err != nil {
				t.Fatalf("selected source kernel modules removed: %v", err)
			}
			if _, err := os.Stat(unused); !os.IsNotExist(err) {
				t.Fatalf("unused modules retained: %v", err)
			}
		})
	}
}

func TestCleanupGRUBSourceRootChangePropagatesToCaller(t *testing.T) {
	setupCleanupGRUBDevices(t)
	needed := makeMarkedModules(t, "6.1.0-old")
	unused := makeMarkedModules(t, "6.2.0-unused")
	writeFileWithContent(t, filepath.Join(filepath.Dir(GrubCfgOutput), "select.cfg"), "search --fs-uuid --set=root other-uuid\n")
	writeFileWithContent(t, GrubCfgOutput, "source $prefix/select.cfg; linux /vmlinuz-linux\n")
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(needed); err != nil {
		t.Fatalf("modules removed after sourced root change: %v", err)
	}
	if _, err := os.Stat(unused); !os.IsNotExist(err) {
		t.Fatalf("unused modules retained: %v", err)
	}
}

func TestCleanupGRUBProbeFailureRetainsModules(t *testing.T) {
	setupCleanupGRUBDevices(t)
	needed := makeMarkedModules(t, "6.1.0-old")
	base := moduleCleanupCommand
	moduleCleanupCommand = func(name string, args ...string) ([]byte, error) {
		if name == "grub-probe" {
			return nil, errors.New("device mapping unavailable")
		}
		return base(name, args...)
	}
	writeFileWithContent(t, GrubCfgOutput, "search --fs-uuid --set=root other-uuid\nlinux /vmlinuz-linux\n")
	if err := cleanupRestoredModuleTrees(); err == nil {
		t.Fatal("probe failure should prevent cleanup")
	}
	if _, err := os.Stat(needed); err != nil {
		t.Fatalf("modules removed after probe failure: %v", err)
	}
}

func TestCleanupGRUBAllowsBootOnRootFilesystem(t *testing.T) {
	setupCleanupGRUBDevices(t)
	writeFileWithContent(t, mountInfoPath, "1 0 0:1 / / rw - ext4 root rw\n")
	base := moduleCleanupCommand
	moduleCleanupCommand = func(name string, args ...string) ([]byte, error) {
		if name == "grub-probe" && args[len(args)-1] == "/" {
			if args[0] == "--target=drive" {
				return []byte("(hd0,gpt1)\n"), nil
			}
			return []byte("host-uuid\n"), nil
		}
		return base(name, args...)
	}
	needed := makeMarkedModules(t, "6.3.0-host")
	unused := makeMarkedModules(t, "6.2.0-unused")
	writeFileWithContent(t, GrubCfgOutput, "set root='hd0,gpt1'\nsearch --fs-uuid --set=root host-uuid\nlinux "+filepath.Join(BootDir, "vmlinuz-linux")+"\n")
	if err := cleanupRestoredModuleTrees(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(needed); err != nil {
		t.Fatalf("root filesystem kernel modules removed: %v", err)
	}
	if _, err := os.Stat(unused); !os.IsNotExist(err) {
		t.Fatalf("unused modules retained: %v", err)
	}
}

func TestCleanupGRUBRejectsUnsupportedCommandAndAssignmentSyntax(t *testing.T) {
	for _, script := range []string{
		`set "prefix=/external"; source $prefix/custom.cfg`,
		`set "config_directory=/external"; source $config_directory/custom.cfg`,
		`set pre"fix"=/external; source $prefix/custom.cfg`,
		`set ro"ot"=hd99,gpt1; linux /vmlinuz-linux`,
		`li"nux" /external/custom-image`,
		`$loader /external/custom-image`,
		`chainloader /external/custom-kernel.efi`,
	} {
		t.Run(script, func(t *testing.T) {
			setupCleanupGRUBDevices(t)
			needed := makeMarkedModules(t, "6.1.0-old")
			writeFileWithContent(t, GrubCfgOutput, script+"\n")
			if err := cleanupRestoredModuleTrees(); err == nil {
				t.Fatal("unsupported script should stop cleanup")
			}
			if _, err := os.Stat(needed); err != nil {
				t.Fatalf("modules removed despite unsupported script: %v", err)
			}
		})
	}
}
