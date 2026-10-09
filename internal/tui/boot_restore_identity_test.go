package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runRestoreIdentityFixture(t *testing.T, dracut bool, cmdline string, archiveNames ...string) (string, bool) {
	return runRestoreIdentityFixtureWithMirror(t, dracut, cmdline, "", archiveNames...)
}

func runRestoreIdentityFixtureWithMirror(t *testing.T, dracut bool, cmdline, mirrorDir string, archiveNames ...string) (string, bool) {
	return runRestoreIdentityFixtureWithStorage(t, dracut, cmdline, restoreStorageFixture{mirrorDir: mirrorDir}, archiveNames...)
}

type restoreStorageFixture struct {
	mirrorDir      string
	separateVar    bool
	fstabContent   string
	readOnlyTarget bool
	separateUsr    bool
	failExtraction bool
}

func runRestoreIdentityFixtureWithStorage(t *testing.T, dracut bool, cmdline string, storage restoreStorageFixture, archiveNames ...string) (string, bool) {
	t.Helper()
	boot, snap, efi, grub := setupDirs(t)
	setTestGlobals(t, boot, snap, efi, grub)
	if storage.mirrorDir != "" {
		EfiDir = storage.mirrorDir
	}
	version := "6.6.7-fixture"
	root := filepath.Join(t.TempDir(), "root")
	varRoot := filepath.Join(root, "var")
	if storage.separateVar {
		varRoot = filepath.Join(t.TempDir(), "separate-var")
		if err := os.MkdirAll(varRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if storage.fstabContent == "" {
			storage.fstabContent = "/dev/bootrecov-var /var btrfs subvol=var 0 0\n"
		}
		writeFileWithContent(t, filepath.Join(root, "etc", "fstab"), storage.fstabContent)
	}
	mountStub := `
findmnt() {
  [ "$4" = -T ] && [ -d "$5" ] || return 1
  case "$3" in
    TARGET) printf '%s\n' "$TEST_TARGET_MOUNT" ;;
    VFS-OPTIONS) printf '%s\n' "$TEST_TARGET_MODE" ;;
    *) return 1 ;;
  esac
}
mount() {
  if [ "$1" = -o ]; then
    [ "$3" = -- ] && [ "$4" = "$TEST_TARGET_MOUNT" ] || return 1
    case "$2" in
      remount,rw) TEST_TARGET_MODE=rw ;;
      remount,ro) TEST_TARGET_MODE=ro ;;
      *) return 1 ;;
    esac
    printf '%s\n' "$TEST_TARGET_MODE" >>"$TEST_MOUNT_TRACE"
    return 0
  fi
  fixture_mount_target=""
  for arg in "$@"; do fixture_mount_target="$arg"; done
  cp -a "$TEST_VAR_DIR/." "$fixture_mount_target/"
}
umount() {
  rm -rf "$1/backups"
}
`
	for _, name := range archiveNames {
		writeFile(t, filepath.Join(varRoot, "backups", "bootrecov-snapshots", name, ".bootrecov", "root-modules", version+".sqfs"))
	}
	var restoreScript, call string
	if dracut {
		restoreScript = "run_restore() {\n" + renderDracutRestoreScript() + "\n}\n"
		call = "run_restore"
	} else {
		restoreScript = renderMkinitcpioRuntimeHook()
		call = "run_latehook"
	}
	script := fmt.Sprintf(`#!/bin/sh
set -e
cat() {
  if [ "${1:-}" = /proc/cmdline ]; then printf '%%s\n' "$TEST_CMDLINE"; return 0; fi
  command cat "$@"
}
uname() {
  if [ "${1:-}" = -r ]; then printf '%%s\n' %s; return 0; fi
  command uname "$@"
}
unsquashfs() {
  [ "${1:-}" = -d ] || return 2
  [ "$TEST_TARGET_MODE" = rw ] && [ "$TEST_FAIL_EXTRACTION" = false ] || return 1
  mkdir -p "$2"
  printf 'restored\n' >"$2/modules.dep"
}
newroot=%s
NEWROOT="$newroot"
%s
%s
%s
`, shellSingleQuote(version), shellSingleQuote(root), mountStub, restoreScript, call)
	scriptPath := filepath.Join(t.TempDir(), "run-restore.sh")
	writeExecutable(t, scriptPath, script)
	cmd := exec.Command("sh", scriptPath)
	varMountBase := t.TempDir()
	mode := "rw"
	if storage.readOnlyTarget {
		mode = "ro"
	}
	targetMount := root
	if storage.separateUsr {
		targetMount = filepath.Join(root, "usr")
		if err := os.MkdirAll(targetMount, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tracePath := filepath.Join(t.TempDir(), "mount-trace")
	cmd.Env = append(os.Environ(), "TEST_CMDLINE="+cmdline, "TEST_VAR_DIR="+varRoot, "TMPDIR="+varMountBase,
		"TEST_TARGET_MODE="+mode, "TEST_TARGET_MOUNT="+targetMount, "TEST_MOUNT_TRACE="+tracePath, fmt.Sprintf("TEST_FAIL_EXTRACTION=%v", storage.failExtraction))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("restore script failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	_, err = os.Stat(filepath.Join(root, strings.TrimPrefix(RootModulesDir, "/"), version, "modules.dep"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if storage.readOnlyTarget {
		trace, readErr := os.ReadFile(tracePath)
		if readErr != nil || string(trace) != "rw\nro\n" {
			t.Fatalf("read-only target mode was not restored: trace=%q err=%v output=%q", trace, readErr, out)
		}
	}
	if storage.separateVar {
		entries, readErr := os.ReadDir(varMountBase)
		if readErr != nil || len(entries) != 0 {
			t.Fatalf("temporary /var mount was not cleaned up: entries=%v err=%v output=%q", entries, readErr, out)
		}
	}
	return string(out), err == nil
}

func TestDracutRestoreReadsArchiveFromSeparateVarSubvolume(t *testing.T) {
	out, restored := runRestoreIdentityFixtureWithStorage(t, true,
		"BOOT_IMAGE=(hd0,gpt3)/boot/custom-recovery/good/vmlinuz", restoreStorageFixture{mirrorDir: "/boot/custom-recovery", separateVar: true}, "good")
	if !restored {
		t.Fatalf("dracut did not restore archive from separately mounted /var: %q", out)
	}
}

func TestDracutRestoreRejectsAmbiguousSeparateVarMount(t *testing.T) {
	out, restored := runRestoreIdentityFixtureWithStorage(t, true,
		"BOOT_IMAGE=(hd0,gpt3)/boot/custom-recovery/good/vmlinuz", restoreStorageFixture{mirrorDir: "/boot/custom-recovery", separateVar: true,
			fstabContent: "/dev/a /var btrfs subvol=var 0 0\n/dev/b /var btrfs subvol=other 0 0\n"}, "good")
	if restored || !strings.Contains(out, "ambiguous /var mount") {
		t.Fatalf("ambiguous /var mounts must not restore modules: restored=%v output=%q", restored, out)
	}
}

func TestDracutRestoreReportsMissingArchiveOnSeparateVar(t *testing.T) {
	out, restored := runRestoreIdentityFixtureWithStorage(t, true,
		"BOOT_IMAGE=(hd0,gpt3)/boot/custom-recovery/good/vmlinuz", restoreStorageFixture{mirrorDir: "/boot/custom-recovery", separateVar: true})
	if restored || !strings.Contains(out, "archived modules unavailable") {
		t.Fatalf("missing /var archive needs a concrete diagnosis: restored=%v output=%q", restored, out)
	}
}

func TestDracutRestoreReadsSeparateVarFstabWithoutFinalNewline(t *testing.T) {
	out, restored := runRestoreIdentityFixtureWithStorage(t, true,
		"BOOT_IMAGE=(hd0,gpt3)/boot/custom-recovery/good/vmlinuz", restoreStorageFixture{mirrorDir: "/boot/custom-recovery", separateVar: true,
			fstabContent: "/dev/bootrecov-var /var btrfs subvol=var 0 0"}, "good")
	if !restored {
		t.Fatalf("dracut skipped final /var fstab entry without newline: %q", out)
	}
}

func TestDracutRestoreTemporarilyRemountsReadOnlyTarget(t *testing.T) {
	for _, separateUsr := range []bool{false, true} {
		for _, failExtraction := range []bool{false, true} {
			t.Run(fmt.Sprintf("separateUsr=%v/extractionFailed=%v", separateUsr, failExtraction), func(t *testing.T) {
				out, restored := runRestoreIdentityFixtureWithStorage(t, true,
					"BOOT_IMAGE=(hd0,gpt3)/boot/custom-recovery/good/vmlinuz", restoreStorageFixture{mirrorDir: "/boot/custom-recovery",
						separateVar: true, readOnlyTarget: true, separateUsr: separateUsr, failExtraction: failExtraction}, "good")
				if restored == failExtraction {
					t.Fatalf("unexpected restore result: extractionFailed=%v restored=%v output=%q", failExtraction, restored, out)
				}
			})
		}
	}
}

func TestDracutRestoreUsesConfiguredCustomMirrorWithoutKernelMarkers(t *testing.T) {
	out, restored := runRestoreIdentityFixtureWithMirror(t, true,
		"BOOT_IMAGE=(hd0,gpt3)/boot/custom-recovery/good/vmlinuz", "/boot/custom-recovery", "good")
	if !restored {
		t.Fatalf("configured custom mirror did not restore modules from markerless BLS boot: %q", out)
	}
}

func TestDracutRestoreIgnoresUnrelatedMarkerlessImages(t *testing.T) {
	for _, image := range []string{
		"(hd0,gpt3)/boot/other/good/vmlinuz",
		"(hd0,gpt3)/boot/custom-recovery-evil/good/vmlinuz",
	} {
		out, restored := runRestoreIdentityFixtureWithMirror(t, true,
			"BOOT_IMAGE="+image, "/boot/custom-recovery", "good")
		if restored {
			t.Fatalf("unrelated markerless image restored modules: image=%q output=%q", image, out)
		}
	}
}

func TestMkinitcpioRestoreRejectsConflictingSnapshotIdentities(t *testing.T) {
	out, restored := runRestoreIdentityFixture(t, false,
		"BOOT_IMAGE=/bootrecov-snapshots/good/vmlinuz bootrecov_entry=bootrecov-abcdef123456 bootrecov_snapshot=bad", "good", "bad")
	if restored || !strings.Contains(out, "conflicting snapshot") {
		t.Fatalf("conflicting identity must not restore modules: restored=%v output=%q", restored, out)
	}
}

func TestDracutRestoreRejectsConflictingSnapshotIdentities(t *testing.T) {
	out, restored := runRestoreIdentityFixture(t, true,
		"BOOT_IMAGE=/bootrecov-snapshots/good/vmlinuz bootrecov_entry=bootrecov-abcdef123456 bootrecov_snapshot=bad", "good", "bad")
	if restored || !strings.Contains(out, "conflicting snapshot") {
		t.Fatalf("conflicting identity must not restore modules: restored=%v output=%q", restored, out)
	}
}

func TestBootRestoreRejectsCustomMirrorImageMarkerConflict(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		out, restored := runRestoreIdentityFixture(t, dracut,
			"BOOT_IMAGE=/custom-recovery/other/vmlinuz bootrecov_entry=bootrecov-abcdef123456 bootrecov_snapshot=good", "good", "other")
		if restored || !strings.Contains(out, "conflicting snapshot") {
			t.Fatalf("custom path conflict must not restore modules: dracut=%v restored=%v output=%q", dracut, restored, out)
		}
	}
}

func TestBootRestoreRejectsInvalidSnapshotMarkers(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		for _, name := range []string{"bad;name", ".hidden", "../escape"} {
			t.Run(fmt.Sprintf("dracut=%v/name=%s", dracut, name), func(t *testing.T) {
				out, restored := runRestoreIdentityFixture(t, dracut,
					"BOOT_IMAGE=/custom-recovery/snap/vmlinuz bootrecov_entry=bootrecov-abcdef123456 bootrecov_snapshot="+name, name)
				if restored || !strings.Contains(out, "invalid snapshot name") {
					t.Fatalf("invalid marker must not restore modules: restored=%v output=%q", restored, out)
				}
			})
		}
	}
}

func TestBootRestoreRejectsDuplicateSnapshotMarkers(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		out, restored := runRestoreIdentityFixture(t, dracut,
			"BOOT_IMAGE=/custom-recovery/good/vmlinuz bootrecov_entry=bootrecov-abcdef123456 bootrecov_snapshot=good bootrecov_snapshot=bad", "good", "bad")
		if restored || !strings.Contains(out, "ambiguous snapshot") {
			t.Fatalf("duplicate marker must not restore modules: dracut=%v restored=%v output=%q", dracut, restored, out)
		}
	}
}

func TestBootRestoreRejectsEmptySnapshotMarker(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		out, restored := runRestoreIdentityFixture(t, dracut,
			"BOOT_IMAGE=/bootrecov-snapshots/good/vmlinuz bootrecov_entry=bootrecov-abcdef123456 bootrecov_snapshot=", "good")
		if restored || !strings.Contains(out, "invalid snapshot name") {
			t.Fatalf("empty marker must not fall back silently: dracut=%v restored=%v output=%q", dracut, restored, out)
		}
	}
}

func TestBootRestoreRejectsMultipleBootImages(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		out, restored := runRestoreIdentityFixture(t, dracut,
			"BOOT_IMAGE=/bootrecov-snapshots/good/vmlinuz BOOT_IMAGE=/bootrecov-snapshots/bad/vmlinuz bootrecov_entry=bootrecov-abcdef123456 bootrecov_snapshot=bad", "good", "bad")
		if restored || !strings.Contains(out, "ambiguous snapshot") {
			t.Fatalf("multiple BOOT_IMAGE values must not restore modules: dracut=%v restored=%v output=%q", dracut, restored, out)
		}
	}
}

func TestBootRestoreAcceptsMatchingMarkerAndLegacyImage(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		for _, cmdline := range []string{
			"BOOT_IMAGE=/bootrecov-snapshots/good/vmlinuz bootrecov_entry=bootrecov-abcdef123456 bootrecov_snapshot=good",
			"BOOT_IMAGE=/bootrecov-snapshots/good/vmlinuz",
		} {
			out, restored := runRestoreIdentityFixture(t, dracut, cmdline, "good")
			if !restored {
				t.Fatalf("valid recovery did not restore modules: dracut=%v cmdline=%q output=%q", dracut, cmdline, out)
			}
		}
	}
}

func TestBootRestoreAcceptsCustomMirrorNestedUnderLegacyDirectory(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		out, restored := runRestoreIdentityFixture(t, dracut,
			"BOOT_IMAGE=/bootrecov-snapshots/custom-recovery/good/vmlinuz bootrecov_entry=bootrecov-abcdef123456 bootrecov_snapshot=good", "good")
		if !restored {
			t.Fatalf("nested custom mirror did not restore modules: dracut=%v output=%q", dracut, out)
		}
	}
}

func TestBootRestoreSupportsExistingCustomMirrorEntryWithoutSnapshotMarker(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		for _, image := range []string{
			"/custom-recovery/good/vmlinuz",
			"/bootrecov-snapshots/custom-recovery/good/vmlinuz",
			"(hd0,gpt2)/custom-recovery/good/vmlinuz",
		} {
			out, restored := runRestoreIdentityFixture(t, dracut,
				"BOOT_IMAGE="+image+" bootrecov_entry=bootrecov-abcdef123456", "good")
			if !restored {
				t.Fatalf("existing custom-mirror entry did not restore modules: dracut=%v image=%q output=%q", dracut, image, out)
			}
		}
	}
}

func TestBootRestoreRejectsTraversalInExistingEntryImagePath(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		for _, image := range []string{
			"/custom-recovery/../good/vmlinuz",
			"/custom-recovery/./good/vmlinuz",
			"/custom-recovery//good/vmlinuz",
		} {
			out, restored := runRestoreIdentityFixture(t, dracut,
				"BOOT_IMAGE="+image+" bootrecov_entry=bootrecov-abcdef123456", "good")
			if restored || !strings.Contains(out, "invalid BOOT_IMAGE path") {
				t.Fatalf("malformed image path must not restore modules: dracut=%v image=%q restored=%v output=%q", dracut, image, restored, out)
			}
		}
	}
}

func TestBootRestoreRejectsImagePathWithoutKernelFilename(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		out, restored := runRestoreIdentityFixture(t, dracut,
			"BOOT_IMAGE=/custom-recovery/good/ bootrecov_entry=bootrecov-abcdef123456 bootrecov_snapshot=good", "good")
		if restored || !strings.Contains(out, "invalid BOOT_IMAGE path") {
			t.Fatalf("image path without kernel filename must not restore: dracut=%v restored=%v output=%q", dracut, restored, out)
		}
	}
}

func TestBootRestoreReportsMissingArchiveForCustomMirror(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		out, restored := runRestoreIdentityFixture(t, dracut,
			"BOOT_IMAGE=/custom-recovery/good/vmlinuz bootrecov_entry=bootrecov-abcdef123456 bootrecov_snapshot=good")
		if restored || !strings.Contains(out, "archived modules unavailable") || !strings.Contains(out, "/good/.bootrecov/root-modules/6.6.7-fixture.sqfs") {
			t.Fatalf("missing archive diagnosis wrong: dracut=%v restored=%v output=%q", dracut, restored, out)
		}
	}
}

func TestBootRestoreIgnoresSnapshotParameterWithoutEntryMarker(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		out, restored := runRestoreIdentityFixture(t, dracut,
			"BOOT_IMAGE=/custom-recovery/good/vmlinuz bootrecov_snapshot=good", "good")
		if restored || !strings.Contains(out, "entry marker") {
			t.Fatalf("snapshot parameter alone must not restore modules: dracut=%v restored=%v output=%q", dracut, restored, out)
		}
	}
}

func TestBootRestoreRejectsAmbiguousOrInvalidEntryMarkers(t *testing.T) {
	for _, dracut := range []bool{false, true} {
		for _, markers := range []string{
			"bootrecov_entry=invalid bootrecov_snapshot=good",
			"bootrecov_entry=bootrecov-abcdef123456 bootrecov_entry=bootrecov-fedcba654321 bootrecov_snapshot=good",
		} {
			out, restored := runRestoreIdentityFixture(t, dracut,
				"BOOT_IMAGE=/custom-recovery/good/vmlinuz "+markers, "good")
			if restored || !strings.Contains(out, "entry marker") {
				t.Fatalf("invalid entry marker must not restore modules: dracut=%v markers=%q restored=%v output=%q", dracut, markers, restored, out)
			}
		}
	}
}
