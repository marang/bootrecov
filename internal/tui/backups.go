package tui

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
)

// BootBackup represents one logical backup snapshot stored in SnapshotDir.
// An optional EFI mirror exists in EfiDir only when the snapshot is activated
// for GRUB booting.
type BootBackup struct {
	Name               string
	Path               string // Canonical path used for GRUB entries (EFI copy)
	SnapshotPath       string
	EFIPath            string
	MetadataPath       string
	HasSnapshot        bool
	HasEFI             bool
	InSync             bool
	KernelImage        string
	InitramfsImage     string
	MicrocodeImages    []string
	KernelVersion      string
	RootModuleTree     string
	RootModulesKnown   bool
	HasRootModules     bool
	ArchivedModuleTree string
	HasArchivedModules bool
	CreatedAt          time.Time
	SizeBytes          int64
	HasKernel          bool
	HasInitramfs       bool
	GrubEntryExists    bool
}

type GrubEntry struct {
	ID         string
	BackupPath string
	Name       string
}

var (
	BootDir                     = "/boot"
	SnapshotDir                 = "/var/backups/bootrecov-snapshots"
	EfiDir                      = "/boot/efi/bootrecov-snapshots"
	GrubCustom                  = "/etc/grub.d/41_bootrecov_snapshots"
	GrubCfgOutput               = "/boot/grub/grub.cfg"
	GrubMkconfig                = "grub-mkconfig"
	AutoUpdateGrub              = true
	RootModulesDir              = "/usr/lib/modules"
	PacmanHookPath              = "/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook"
	PacmanPostHookPath          = "/etc/pacman.d/hooks/96-bootrecov-post-transaction.hook"
	MkinitcpioInstallPath       = "/usr/lib/initcpio/install/bootrecov"
	MkinitcpioHookPath          = "/usr/lib/initcpio/hooks/bootrecov"
	MkinitcpioConfPath          = "/etc/mkinitcpio.conf"
	MkinitcpioBin               = "mkinitcpio"
	BLSEntriesDir               = "/boot/loader/entries"
	DNF5ActionsPath             = "/etc/dnf/libdnf5-plugins/actions.d/95-bootrecov.actions"
	DNF4PreActionsPath          = "/etc/dnf/plugins/pre-transaction-actions.d/95-bootrecov.action"
	DNF4PostActionsPath         = "/etc/dnf/plugins/post-transaction-actions.d/95-bootrecov.action"
	DracutModuleDir             = "/usr/lib/dracut/modules.d/95bootrecov"
	DracutBin                   = "dracut"
	UpdateInitramfs             = true
	RcloneBin                   = "rclone"
	RequireRclone               = true
	MksquashfsBin               = "mksquashfs"
	RequireMksquashfs           = true
	UnsquashfsBin               = "unsquashfs"
	RequireUnsquashfs           = true
	RequireEFIMount             = true
	BackupProfile               = "full" // full|minimal
	grubHeader                  = "#!/bin/bash\n"
	statfsFunc                  = syscall.Statfs
	mountInfoPath               = "/proc/self/mountinfo"
	kernelCmdlinePath           = "/proc/cmdline"
	createModuleImageFunc       = createSquashFSModuleImage
	restoreModuleTreeFunc       = restoreSquashFSModuleTree
	chownRestoredModuleTreeFunc = chownTreeToRoot
)

const (
	backupNameCurrentDir  = "."
	backupNameParentDir   = ".."
	kernelCmdlineMarker   = "bootrecov_entry="
	bootrecovMetadataRoot = ".bootrecov"
	moduleArchiveRoot     = ".bootrecov/root-modules"
	lowFreeSpaceThreshold = int64(1024 * 1024)
)

var backupNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// CreateBootBackupNow copies the current /boot tree to SnapshotDir.
// EFI mirrors are created only when explicitly activated. The backup name is a
// UTC timestamp.
func CreateBootBackupNow() (BootBackup, error) {
	return withRecoveryBackupOperation(createBootBackupNow)
}

func createBootBackupNow() (BootBackup, error) {
	if err := checkSnapshotSpace(); err != nil {
		return BootBackup{}, err
	}
	if err := validateBootSourceForSnapshot(); err != nil {
		return BootBackup{}, err
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	snapshotTarget := filepath.Join(SnapshotDir, stamp)
	stagingTarget := filepath.Join(SnapshotDir, ".tmp-"+stamp)

	if dirExists(snapshotTarget) {
		return BootBackup{}, fmt.Errorf("%w: snapshot already exists: %s", ErrSyncFailed, snapshotTarget)
	}
	_ = os.RemoveAll(stagingTarget)
	if err := os.MkdirAll(stagingTarget, 0o755); err != nil {
		return BootBackup{}, wrapFilesystemWriteError(stagingTarget, err)
	}
	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			_ = os.RemoveAll(stagingTarget)
		}
	}()

	if err := copyBootSourceToSnapshot(stagingTarget); err != nil {
		return BootBackup{}, err
	}
	created := buildBackupFromPaths(stamp, stagingTarget, filepath.Join(EfiDir, stamp))
	refreshBackupCompleteness(&created)
	if !created.HasKernel || !created.HasInitramfs {
		return BootBackup{}, fmt.Errorf("%w: copied snapshot %q is missing required boot artifacts", ErrBackupIncomplete, stamp)
	}
	if err := archiveRootModulesForSnapshot(&created); err != nil {
		return BootBackup{}, err
	}
	if err := os.Rename(stagingTarget, snapshotTarget); err != nil {
		return BootBackup{}, wrapFilesystemWriteError(snapshotTarget, err)
	}
	cleanupStaging = false
	created = buildBackupFromName(stamp)
	refreshBackupCompleteness(&created)
	return created, nil
}

func InstallPlatformHooks(executablePath string) error {
	switch currentPlatformID() {
	case PlatformArch:
		return InstallPacmanHook(executablePath)
	case PlatformFedora:
		return installFedoraHooks(executablePath)
	default:
		return ensurePlatformHookSupported()
	}
}

func UninstallPlatformHooks() (bool, error) {
	switch currentPlatformID() {
	case PlatformArch:
		return UninstallPacmanHook()
	case PlatformFedora:
		return uninstallFedoraHooks()
	default:
		if err := ensurePlatformHookSupported(); err != nil {
			return false, err
		}
		return false, nil
	}
}

func PlatformHooksInstalled() bool {
	switch currentPlatformID() {
	case PlatformArch:
		return HookInstalled()
	case PlatformFedora:
		return fedoraHooksInstalled()
	default:
		return false
	}
}

func validateHookExecutablePath(executablePath string) (string, error) {
	if strings.TrimSpace(executablePath) == "" {
		executablePath = defaultHookExecutablePath()
	}
	executablePath = filepath.Clean(strings.TrimSpace(executablePath))
	if !filepath.IsAbs(executablePath) {
		return "", &HookExecutablePathError{Path: executablePath, Reason: "hook executable path must be absolute"}
	}
	if strings.IndexFunc(executablePath, unicode.IsSpace) >= 0 {
		return "", &HookExecutablePathError{Path: executablePath, Reason: "hook executable path must not contain whitespace"}
	}
	return executablePath, nil
}

func InstallPacmanHook(executablePath string) (err error) {
	if currentPlatformID() != PlatformArch {
		return fmt.Errorf("%w: pacman hook install is only supported on Arch; use bootrecov hook install for platform-specific hooks", ErrUnsupportedPackageHook)
	}
	if err := ensurePlatformHookSupported(); err != nil {
		return err
	}
	if err := ensureInitramfsHookSupported(); err != nil {
		return err
	}
	var pathErr error
	executablePath, pathErr = validateHookExecutablePath(executablePath)
	if pathErr != nil {
		return pathErr
	}
	snapshots, err := snapshotInstallPaths([]string{PacmanHookPath, PacmanPostHookPath, MkinitcpioInstallPath, MkinitcpioHookPath, MkinitcpioConfPath})
	if err != nil {
		return err
	}
	installed := false
	defer func() {
		if err != nil && !installed {
			if rollbackErr := restoreInstallPathSnapshots(snapshots); rollbackErr != nil {
				err = errors.Join(err, rollbackErr)
			}
		}
	}()
	for _, hookPath := range []string{PacmanHookPath, PacmanPostHookPath, MkinitcpioInstallPath, MkinitcpioHookPath} {
		if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(PacmanHookPath, []byte(renderPacmanPreHook(executablePath)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(PacmanPostHookPath, []byte(renderPacmanPostHook(executablePath)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(MkinitcpioInstallPath, []byte(renderMkinitcpioInstallHook()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(MkinitcpioHookPath, []byte(renderMkinitcpioRuntimeHook()), 0o644); err != nil {
		return err
	}
	if err := enableMkinitcpioHook(); err != nil {
		return err
	}
	if err := regenerateInitramfs(); err != nil {
		return err
	}
	installed = true
	return nil
}

func UninstallPacmanHook() (bool, error) {
	if currentPlatformID() != PlatformArch {
		return false, fmt.Errorf("%w: pacman hook uninstall is only supported on Arch; use bootrecov hook uninstall for platform-specific hooks", ErrUnsupportedPackageHook)
	}
	removed := false
	for _, hookPath := range []string{PacmanHookPath, PacmanPostHookPath, MkinitcpioInstallPath, MkinitcpioHookPath} {
		if err := os.Remove(hookPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, err
		}
		removed = true
	}
	confChanged, err := disableMkinitcpioHook()
	if err != nil {
		return removed, err
	}
	if confChanged {
		removed = true
		if err := regenerateInitramfs(); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func HookInstalled() bool {
	for _, hookPath := range []string{PacmanHookPath, PacmanPostHookPath, MkinitcpioInstallPath, MkinitcpioHookPath} {
		if !fileExists(hookPath) {
			return false
		}
	}
	data, err := os.ReadFile(MkinitcpioConfPath)
	if err != nil {
		return false
	}
	return mkinitcpioHookEnabled(data)
}

func renderPacmanPreHook(executablePath string) string {
	return fmt.Sprintf(`[Trigger]
Operation = Install
Operation = Upgrade
Operation = Remove
Type = Package
Target = linux*
Target = grub
Target = mkinitcpio
Target = systemd

[Action]
Description = Creating bootrecov snapshot before boot-critical package transaction...
When = PreTransaction
Exec = /usr/bin/env BOOTRECOV_ACCEPT_RISK=1 %s hook backup-now
`, executablePath)
}

func renderPacmanPostHook(executablePath string) string {
	return fmt.Sprintf(`[Trigger]
Operation = Install
Operation = Upgrade
Operation = Remove
Type = Package
Target = linux*
Target = grub
Target = mkinitcpio
Target = systemd

[Action]
Description = Restoring active bootrecov fallback module trees after boot-critical package transaction...
When = PostTransaction
Exec = /usr/bin/env BOOTRECOV_ACCEPT_RISK=1 %s hook reconcile-active
`, executablePath)
}

func renderMkinitcpioInstallHook() string {
	return `build() {
    add_binary /usr/bin/unsquashfs
    add_binary /usr/bin/stat
    add_runscript
}

help() {
    cat <<'HELPEOF'
Restores archived Bootrecov kernel modules during fallback boots.
HELPEOF
}
`
}

func renderMkinitcpioRuntimeHook() string {
	rootModulesDir := filepath.Clean(RootModulesDir)
	if !filepath.IsAbs(rootModulesDir) {
		rootModulesDir = "/usr/lib/modules"
	}
	return fmt.Sprintf(`run_latehook() {
    local cmdline entry boot_image has_marker name version root_modules_dir archive modules_parent target staging inode

    cmdline="$(cat /proc/cmdline 2>/dev/null || true)"
    has_marker=0
    case " ${cmdline} " in
        *" bootrecov_entry="*) has_marker=1 ;;
    esac

    for entry in ${cmdline}; do
        case "${entry}" in
            BOOT_IMAGE=/bootrecov-snapshots/*)
                boot_image="${entry#BOOT_IMAGE=/bootrecov-snapshots/}"
                name="${boot_image%%/*}"
                ;;
			BOOT_IMAGE=*/bootrecov-snapshots/*)
				boot_image="${entry#*/bootrecov-snapshots/}"
				name="${boot_image%%/*}"
				;;
        esac
    done

    [ -n "${name}" ] || {
        [ "${has_marker}" -eq 0 ] || echo "bootrecov: fallback marker found but snapshot name could not be parsed" >&2
        return 0
    }

    case "${name}" in
        .|..|/*|*/*|*" "*) echo "bootrecov: refusing invalid snapshot name: ${name}" >&2; return 0 ;;
    esac

    version="$(uname -r)"
    newroot="${newroot:-/new_root}"
    root_modules_dir=%s
    archive="${newroot}/var/backups/bootrecov-snapshots/${name}/.bootrecov/root-modules/${version}.sqfs"
    modules_parent="${newroot}${root_modules_dir}"
    target="${modules_parent}/${version}"

    [ ! -d "${target}" ] || return 0
    [ -s "${archive}" ] || {
        echo "bootrecov: archived modules unavailable: ${archive}" >&2
        return 0
    }

    mkdir -p "${modules_parent}" || {
        echo "bootrecov: cannot create ${modules_parent}" >&2
        return 0
    }

    staging="$(mktemp -d "${modules_parent}/.bootrecov-restore.XXXXXX" 2>/dev/null)" || {
        echo "bootrecov: cannot create module restore staging directory" >&2
        return 0
    }

    if ! unsquashfs -d "${staging}" "${archive}" >/dev/null 2>&1; then
        echo "bootrecov: failed to restore modules from ${archive}" >&2
        rm -rf "${staging}"
        return 0
    fi

    if [ -d "${target}" ]; then
        rm -rf "${staging}"
        return 0
    fi

    inode="$(stat -c '%%i' "${staging}" 2>/dev/null)"
    case "${inode}" in
        ''|*[!0-9]*) echo "bootrecov: cannot identify restored module directory" >&2; rm -rf "${staging}"; return 0 ;;
    esac
    if ! rm -f "${staging}/%s" || ! printf '%%s\n%%s\n' "${version}" "${inode}" > "${staging}/%s"; then
        echo "bootrecov: cannot mark restored modules" >&2
        rm -rf "${staging}"
        return 0
    fi

    if mv "${staging}" "${target}"; then
        chown -R 0:0 "${target}" 2>/dev/null || true
        echo "bootrecov: restored modules for ${version} from ${name}" >&2
    else
        echo "bootrecov: failed to move restored modules into ${target}" >&2
        rm -rf "${staging}"
    fi
}
`, shellSingleQuote(rootModulesDir), restoredModuleMarker, restoredModuleMarker)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func enableMkinitcpioHook() error {
	data, err := os.ReadFile(MkinitcpioConfPath)
	if err != nil {
		return err
	}
	updated, changed := updateMkinitcpioHooks(data, true)
	if !changed {
		if mkinitcpioHookEnabled(data) {
			return nil
		}
		return fmt.Errorf("%w: could not find editable HOOKS=(...) line in %s", ErrUnsupportedInitramfsHook, MkinitcpioConfPath)
	}
	if err := backupMkinitcpioConf(data); err != nil {
		return err
	}
	return writeFilePreservingMode(MkinitcpioConfPath, updated, 0o644)
}

func disableMkinitcpioHook() (bool, error) {
	data, err := os.ReadFile(MkinitcpioConfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	updated, changed := updateMkinitcpioHooks(data, false)
	if !changed {
		return false, nil
	}
	if err := backupMkinitcpioConf(data); err != nil {
		return false, err
	}
	return true, writeFilePreservingMode(MkinitcpioConfPath, updated, 0o644)
}

func backupMkinitcpioConf(data []byte) error {
	backupPath := fmt.Sprintf("%s.bootrecov-%s", MkinitcpioConfPath, time.Now().UTC().Format("20060102-150405"))
	return os.WriteFile(backupPath, data, 0o644)
}

type installPathSnapshot struct {
	path    string
	data    []byte
	mode    os.FileMode
	existed bool
}

func snapshotInstallPaths(paths []string) ([]installPathSnapshot, error) {
	snapshots := make([]installPathSnapshot, 0, len(paths))
	for _, path := range paths {
		snapshot := installPathSnapshot{path: path}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				snapshots = append(snapshots, snapshot)
				continue
			}
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("refusing to overwrite directory: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		snapshot.data = data
		snapshot.mode = info.Mode().Perm()
		snapshot.existed = true
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func restoreInstallPathSnapshots(snapshots []installPathSnapshot) error {
	var errs []error
	for _, snapshot := range snapshots {
		if snapshot.existed {
			if err := os.MkdirAll(filepath.Dir(snapshot.path), 0o755); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := os.WriteFile(snapshot.path, snapshot.data, snapshot.mode); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if err := os.Remove(snapshot.path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func updateMkinitcpioHooks(data []byte, enable bool) ([]byte, bool) {
	lines := strings.SplitAfter(string(data), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, "HOOKS=") {
			continue
		}
		open := strings.Index(line, "(")
		close := strings.LastIndex(line, ")")
		if open == -1 || close == -1 || close < open {
			continue
		}
		prefix := line[:open+1]
		suffix := line[close:]
		trailingNewline := ""
		if strings.HasSuffix(suffix, "\n") {
			trailingNewline = "\n"
			suffix = strings.TrimSuffix(suffix, "\n")
		}
		hooks := strings.Fields(line[open+1 : close])
		updatedHooks, changed := updateHookList(hooks, enable)
		if !changed {
			return data, false
		}
		lines[i] = prefix + strings.Join(updatedHooks, " ") + suffix + trailingNewline
		return []byte(strings.Join(lines, "")), true
	}
	return data, false
}

func mkinitcpioHookEnabled(data []byte) bool {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, "HOOKS=") {
			continue
		}
		open := strings.Index(line, "(")
		close := strings.LastIndex(line, ")")
		if open == -1 || close == -1 || close < open {
			continue
		}
		for _, hook := range strings.Fields(line[open+1 : close]) {
			if hook == "bootrecov" {
				return true
			}
		}
	}
	return false
}

func writeFilePreservingMode(path string, data []byte, fallback os.FileMode) error {
	mode := fallback
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return os.WriteFile(path, data, mode)
}

func updateHookList(hooks []string, enable bool) ([]string, bool) {
	hasBootrecov := false
	for _, hook := range hooks {
		if hook == "bootrecov" {
			hasBootrecov = true
			break
		}
	}
	if enable {
		if hasBootrecov {
			return hooks, false
		}
		out := make([]string, 0, len(hooks)+1)
		inserted := false
		for _, hook := range hooks {
			out = append(out, hook)
			if hook == "filesystems" {
				out = append(out, "bootrecov")
				inserted = true
			}
		}
		if !inserted {
			out = append(out, "bootrecov")
		}
		return out, true
	}
	if !hasBootrecov {
		return hooks, false
	}
	out := make([]string, 0, len(hooks))
	for _, hook := range hooks {
		if hook != "bootrecov" {
			out = append(out, hook)
		}
	}
	return out, true
}

func regenerateInitramfs() error {
	if !UpdateInitramfs {
		return nil
	}
	if strings.TrimSpace(MkinitcpioBin) == "" {
		return fmt.Errorf("%w: mkinitcpio is required but not configured", ErrRequiredToolUnavailable)
	}
	if _, err := exec.LookPath(MkinitcpioBin); err != nil {
		return fmt.Errorf("%w: mkinitcpio is required for initramfs regeneration but was not found in PATH", ErrRequiredToolUnavailable)
	}
	cmd := exec.Command(MkinitcpioBin, "-P")
	out, err := runCommandCombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("%w: mkinitcpio -P: %w: %s", ErrCommandFailed, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func defaultHookExecutablePath() string {
	if fileExists("/usr/bin/bootrecov") {
		return "/usr/bin/bootrecov"
	}
	if exe, err := os.Executable(); err == nil && filepath.IsAbs(exe) {
		return exe
	}
	return "/usr/bin/bootrecov"
}

func CheckRuntimeDependencies() error {
	var missing []string
	if err := ensureSupportedBootloader(); err != nil {
		missing = append(missing, fmt.Sprintf("bootloader %q (not supported yet)", currentBootloaderID()))
	}
	if RequireRclone {
		if strings.TrimSpace(RcloneBin) == "" {
			missing = append(missing, "rclone (not configured)")
		} else if _, err := exec.LookPath(RcloneBin); err != nil {
			missing = append(missing, fmt.Sprintf("%s (required for snapshot and EFI sync)", RcloneBin))
		}
	}
	if AutoUpdateGrub && currentBootloaderID() == BootloaderGRUB {
		if strings.TrimSpace(GrubMkconfig) == "" {
			missing = append(missing, "grub-mkconfig (not configured)")
		} else if _, err := exec.LookPath(GrubMkconfig); err != nil {
			missing = append(missing, fmt.Sprintf("%s (required to regenerate %s)", GrubMkconfig, GrubCfgOutput))
		}
	}
	if RequireMksquashfs {
		if strings.TrimSpace(MksquashfsBin) == "" {
			missing = append(missing, "mksquashfs (not configured)")
		} else if _, err := exec.LookPath(MksquashfsBin); err != nil {
			missing = append(missing, fmt.Sprintf("%s (required to archive kernel modules)", MksquashfsBin))
		}
	}
	if RequireUnsquashfs {
		if strings.TrimSpace(UnsquashfsBin) == "" {
			missing = append(missing, "unsquashfs (not configured)")
		} else if _, err := exec.LookPath(UnsquashfsBin); err != nil {
			missing = append(missing, fmt.Sprintf("%s (required to restore archived kernel modules)", UnsquashfsBin))
		}
	}
	if currentPlatformID() == PlatformArch {
		if _, err := exec.LookPath("file"); err != nil {
			missing = append(missing, "file (required to identify primary kernels during restored module cleanup)")
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return &RuntimeDependenciesError{Missing: missing}
}

// RefreshBackupsAndGrub loads current backups and GRUB entries without
// modifying backup directories or GRUB content.
func RefreshBackupsAndGrub() ([]BootBackup, []GrubEntry, error) {
	if err := ensureSupportedBootloader(); err != nil {
		return nil, nil, err
	}
	backups, err := DiscoverBackups()
	if err != nil {
		return nil, nil, err
	}
	entries, err := ListGrubEntries()
	if err != nil {
		return nil, nil, err
	}
	markGrubFlags(backups, entries)
	return backups, entries, nil
}

// ReconcileCleanupWarning reports incomplete module cleanup after EFI mirrors
// and bootloader entries have been reconciled. The returned lists are current.
type ReconcileCleanupWarning struct {
	Cause error
}

func (e *ReconcileCleanupWarning) Error() string {
	return fmt.Sprintf("reconcile complete; restored module cleanup incomplete: %v", e.Cause)
}

func (e *ReconcileCleanupWarning) Unwrap() error { return e.Cause }

// SyncBackupsAndGrub reconciles optional EFI mirrors used by GRUB entries:
// - keeps EFI mirrors only for activated snapshots
// - refreshes active EFI mirrors from snapshot source
// - removes stale GRUB entries
func SyncBackupsAndGrub() ([]BootBackup, []GrubEntry, error) {
	return withRecoverySyncOperation(syncBackupsAndGrub)
}

func syncBackupsAndGrub() ([]BootBackup, []GrubEntry, error) {
	if err := ensureSupportedBootloader(); err != nil {
		return nil, nil, err
	}
	backups, err := DiscoverBackups()
	if err != nil {
		return nil, nil, err
	}
	entries, err := ListGrubEntries()
	if err != nil {
		return nil, nil, err
	}
	activeByName := map[string]struct{}{}
	for _, e := range entries {
		if e.Name != "" {
			activeByName[e.Name] = struct{}{}
		}
	}
	needsEFIMount := len(activeByName) > 0
	if !needsEFIMount {
		for _, b := range backups {
			if b.HasEFI {
				needsEFIMount = true
				break
			}
		}
	}
	if needsEFIMount {
		if err := ensureEFIMountAvailable(); err != nil {
			return nil, nil, err
		}
	}
	preserveGrubForName := map[string]struct{}{}
	for i := range backups {
		b := &backups[i]
		if !b.HasSnapshot && b.HasEFI {
			if rmErr := os.RemoveAll(b.EFIPath); rmErr != nil {
				refreshBackupCompleteness(b)
				b.InSync = false
				continue
			}
			refreshBackupCompleteness(b)
			continue
		}
		if !b.HasSnapshot {
			b.InSync = false
			continue
		}
		_, isActivated := activeByName[b.Name]
		if !isActivated && b.HasEFI {
			if err := os.RemoveAll(b.EFIPath); err != nil {
				refreshBackupCompleteness(b)
				b.InSync = false
				continue
			} else {
				b.HasEFI = false
			}
			refreshBackupCompleteness(b)
			continue
		}
		wasBootable := IsBootReady(*b)
		if isActivated {
			if err := ensureRootModulesAvailable(b); err != nil {
				refreshBackupCompleteness(b)
				b.InSync = false
				continue
			}
			if err := ensureEFIMirrorFromSnapshot(b); err != nil {
				if wasBootable {
					preserveGrubForName[b.Name] = struct{}{}
				}
				refreshBackupCompleteness(b)
				b.InSync = false
				continue
			}
		}
		refreshBackupCompleteness(b)
	}
	if err := removeStaleGrubEntries(backups, preserveGrubForName); err != nil {
		return nil, nil, err
	}
	cleanupErr := cleanupRestoredModuleTrees()
	// Cleanup can remove modules for inactive archived snapshots. Refresh only
	// module availability so failed mirror sync flags remain intact.
	for i := range backups {
		b := &backups[i]
		b.RootModuleTree, b.RootModulesKnown, b.HasRootModules = detectRootModuleTree(b.KernelVersion)
	}
	entries, err = ListGrubEntries()
	if err != nil {
		return nil, nil, err
	}
	if entries == nil {
		entries = []GrubEntry{}
	}
	markGrubFlags(backups, entries)
	if cleanupErr != nil {
		return backups, entries, &ReconcileCleanupWarning{Cause: cleanupErr}
	}
	return backups, entries, nil
}

// DiscoverBackups returns one row per snapshot name without mutating state.
func DiscoverBackups() ([]BootBackup, error) {
	names, err := listBackupNames()
	if err != nil {
		return nil, err
	}

	backups := make([]BootBackup, 0, len(names))
	for _, name := range names {
		b := buildBackupFromName(name)
		b.InSync = b.HasSnapshot && b.HasEFI
		refreshBackupCompleteness(&b)
		backups = append(backups, b)
	}
	return backups, nil
}

func listBackupNames() ([]string, error) {
	nameSet := map[string]struct{}{}
	for _, root := range []string{SnapshotDir, EfiDir} {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() && validateBackupName(e.Name()) == nil {
				nameSet[e.Name()] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func validateBackupName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return &BackupNameError{Name: name}
	}
	if filepath.IsAbs(name) || name != filepath.Base(name) || name == backupNameCurrentDir || name == backupNameParentDir {
		return &BackupNameError{Name: name}
	}
	if !backupNamePattern.MatchString(name) {
		return &BackupNameError{Name: name}
	}
	return nil
}

func buildBackupFromName(name string) BootBackup {
	name = strings.TrimSpace(name)
	snapshotPath := filepath.Join(SnapshotDir, name)
	efiPath := filepath.Join(EfiDir, name)
	return buildBackupFromPaths(name, snapshotPath, efiPath)
}

func buildBackupFromPaths(name, snapshotPath, efiPath string) BootBackup {
	hasSnapshot := dirExists(snapshotPath)
	hasEFI := dirExists(efiPath)
	return BootBackup{
		Name:         name,
		Path:         snapshotPath,
		SnapshotPath: snapshotPath,
		EFIPath:      efiPath,
		HasSnapshot:  hasSnapshot,
		HasEFI:       hasEFI,
		InSync:       hasSnapshot,
	}
}

func validateBootSourceForSnapshot() error {
	kernel, initramfs := findKernelAndInitramfs(BootDir)
	if kernel == "" || initramfs == "" {
		return fmt.Errorf("%w: %s is missing a required kernel/initramfs pair", ErrBackupIncomplete, BootDir)
	}
	for _, name := range append(findMicrocodeImages(BootDir), kernel, initramfs) {
		path := filepath.Join(BootDir, name)
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("%w: cannot read required boot artifact %s: %w", ErrBackupIncomplete, path, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("%w: cannot read required boot artifact %s: %w", ErrBackupIncomplete, path, err)
		}
	}
	return nil
}

func refreshBackupCompleteness(b *BootBackup) {
	b.HasSnapshot = dirExists(b.SnapshotPath)
	b.HasEFI = dirExists(b.EFIPath)
	if !b.HasSnapshot {
		b.InSync = false
	} else if !b.HasEFI {
		b.InSync = true
	}
	b.Path = b.SnapshotPath
	b.MetadataPath = chooseMetadataPath(b)
	b.KernelImage, b.InitramfsImage = findKernelAndInitramfs(b.MetadataPath)
	b.MicrocodeImages = findMicrocodeImages(b.MetadataPath)
	b.KernelVersion = detectKernelVersion(b.MetadataPath, b.KernelImage, b.InitramfsImage)
	if b.KernelVersion == "unknown" {
		if archivedVersion := detectArchivedKernelVersion(b.SnapshotPath); archivedVersion != "" {
			b.KernelVersion = archivedVersion
		}
	}
	b.RootModuleTree, b.RootModulesKnown, b.HasRootModules = detectRootModuleTree(b.KernelVersion)
	b.ArchivedModuleTree, b.HasArchivedModules = detectArchivedModuleTree(b.SnapshotPath, b.KernelVersion)
	b.CreatedAt = detectBackupTime(b.Name, b.SnapshotPath, b.EFIPath)
	b.SizeBytes = dirSizeBytes(b.MetadataPath)
	b.HasKernel = b.KernelImage != ""
	b.HasInitramfs = b.InitramfsImage != ""
	if b.HasSnapshot && b.HasEFI {
		b.InSync = efiMirrorHasBootArtifacts(*b)
	}
}

func detectRootModuleTree(kernelVersion string) (string, bool, bool) {
	version := strings.TrimSpace(kernelVersion)
	if version == "" || version == "unknown" {
		return "", false, false
	}
	path := filepath.Join(RootModulesDir, version)
	return path, true, dirExists(path)
}

func detectArchivedModuleTree(snapshotPath, kernelVersion string) (string, bool) {
	version := strings.TrimSpace(kernelVersion)
	if snapshotPath == "" || version == "" || version == "unknown" {
		return "", false
	}
	path := archivedModuleImagePath(snapshotPath, version)
	return path, fileExists(path)
}

func detectArchivedKernelVersion(snapshotPath string) string {
	root := filepath.Join(snapshotPath, moduleArchiveRoot)
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var versions []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sqfs") {
			versions = append(versions, strings.TrimSuffix(entry.Name(), ".sqfs"))
		}
	}
	if len(versions) != 1 {
		return ""
	}
	return versions[0]
}

func archivedModuleImagePath(snapshotPath, kernelVersion string) string {
	return filepath.Join(snapshotPath, moduleArchiveRoot, kernelVersion+".sqfs")
}

func IsBootReady(b BootBackup) bool {
	return b.HasSnapshot && b.HasEFI && b.HasKernel && b.HasInitramfs && b.InSync && !hasKnownMissingRootModules(b)
}

func IsRestoreReady(b BootBackup) bool {
	return b.HasSnapshot && b.HasEFI && b.HasKernel && b.HasInitramfs && b.InSync && hasKnownMissingRootModules(b) && b.HasArchivedModules
}

func hasKnownMissingRootModules(b BootBackup) bool {
	return b.RootModulesKnown && !b.HasRootModules
}

func rootModuleStatus(b BootBackup) string {
	if !b.RootModulesKnown {
		return "unknown"
	}
	if b.HasRootModules {
		return "yes"
	}
	if b.HasArchivedModules {
		return "archived"
	}
	return "missing"
}

func validateRootModuleCompatibility(b BootBackup) error {
	if !hasKnownMissingRootModules(b) {
		return nil
	}
	if b.HasArchivedModules {
		return fmt.Errorf(
			"%w: snapshot %q uses kernel %s and has archived modules at %s, but matching root module tree is missing: %s; activate the snapshot to restore archived modules automatically",
			ErrArchivedModulesUnsafe,
			b.Name,
			b.KernelVersion,
			b.ArchivedModuleTree,
			b.RootModuleTree,
		)
	}
	return fmt.Errorf(
		"%w: snapshot %q uses kernel %s but matching root module tree is missing: %s; booting this entry is likely to enter emergency or maintenance mode",
		ErrRootModulesMissing,
		b.Name,
		b.KernelVersion,
		b.RootModuleTree,
	)
}

func ensureRootModulesAvailable(b *BootBackup) error {
	if !hasKnownMissingRootModules(*b) {
		return nil
	}
	if !b.HasArchivedModules {
		return validateRootModuleCompatibility(*b)
	}
	if err := restoreModuleTreeFunc(b.ArchivedModuleTree, b.RootModuleTree); err != nil {
		return fmt.Errorf("%w: restore modules for kernel %s from %s to %s: %w", ErrRootModuleRestoreFailed, b.KernelVersion, b.ArchivedModuleTree, b.RootModuleTree, err)
	}
	b.RootModuleTree, b.RootModulesKnown, b.HasRootModules = detectRootModuleTree(b.KernelVersion)
	if !b.HasRootModules {
		return fmt.Errorf("%w: restored module tree is missing after restore: %s", ErrRootModuleRestoreFailed, b.RootModuleTree)
	}
	return nil
}

func archiveRootModulesForSnapshot(b *BootBackup) error {
	if b.HasSnapshot && !b.RootModulesKnown {
		if version := currentRunningKernelVersion(); version != "" {
			b.KernelVersion = version
			b.RootModuleTree, b.RootModulesKnown, b.HasRootModules = detectRootModuleTree(version)
		}
	}
	if !b.HasSnapshot || !b.RootModulesKnown || !b.HasRootModules {
		return nil
	}
	dst := archivedModuleImagePath(b.SnapshotPath, b.KernelVersion)
	if err := createModuleImageFunc(b.RootModuleTree, dst); err != nil {
		return fmt.Errorf("%w: archive kernel modules for %s: %w", ErrModuleArchiveFailed, b.KernelVersion, err)
	}
	b.ArchivedModuleTree = dst
	b.HasArchivedModules = true
	return nil
}

func createSquashFSModuleImage(src, dst string) error {
	if strings.TrimSpace(MksquashfsBin) == "" {
		return fmt.Errorf("%w: mksquashfs is required but not configured", ErrRequiredToolUnavailable)
	}
	if _, err := exec.LookPath(MksquashfsBin); err != nil {
		return fmt.Errorf("%w: mksquashfs is required for module archive but was not found in PATH", ErrRequiredToolUnavailable)
	}
	if !dirExists(src) {
		return fmt.Errorf("%w: source module tree does not exist: %s", ErrSourceDirectoryMissing, src)
	}
	if fileExists(dst) {
		return fmt.Errorf("%w: %s", ErrModuleArchiveExists, dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return wrapFilesystemWriteError(dst, err)
	}
	cmd := exec.Command(MksquashfsBin, src, dst, "-comp", "zstd", "-Xcompression-level", "15", "-noappend", "-all-root")
	out, err := runCommandCombinedOutput(cmd)
	if err != nil {
		commandErr := fmt.Errorf("%w: mksquashfs: %w: %s", ErrCommandFailed, err, strings.TrimSpace(string(out)))
		return wrapExternalWriteFailure(dst, commandErr)
	}
	return nil
}

func restoreSquashFSModuleTree(archivePath, moduleTreePath string) error {
	if strings.TrimSpace(UnsquashfsBin) == "" {
		return fmt.Errorf("%w: unsquashfs is required but not configured", ErrRequiredToolUnavailable)
	}
	if _, err := exec.LookPath(UnsquashfsBin); err != nil {
		return fmt.Errorf("%w: unsquashfs is required for module restore but was not found in PATH", ErrRequiredToolUnavailable)
	}
	if !fileExists(archivePath) {
		return fmt.Errorf("%w: module archive does not exist: %s", ErrSourceDirectoryMissing, archivePath)
	}
	moduleTreePath = filepath.Clean(moduleTreePath)
	rootModulesDir := filepath.Clean(RootModulesDir)
	if filepath.Dir(moduleTreePath) != rootModulesDir {
		return fmt.Errorf("%w: refusing to restore module tree outside %s: %s", ErrRootModuleRestoreFailed, rootModulesDir, moduleTreePath)
	}
	if dirExists(moduleTreePath) {
		return nil
	}
	if err := os.MkdirAll(rootModulesDir, 0o755); err != nil {
		return wrapFilesystemWriteError(rootModulesDir, err)
	}
	staging, err := os.MkdirTemp(rootModulesDir, ".bootrecov-restore-*")
	if err != nil {
		return wrapFilesystemWriteError(rootModulesDir, err)
	}
	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			_ = os.RemoveAll(staging)
		}
	}()

	cmd := exec.Command(UnsquashfsBin, "-d", staging, archivePath)
	out, err := runCommandCombinedOutput(cmd)
	if err != nil {
		commandErr := fmt.Errorf("%w: unsquashfs: %w: %s", ErrCommandFailed, err, strings.TrimSpace(string(out)))
		return wrapExternalWriteFailure(moduleTreePath, commandErr)
	}
	if dirExists(moduleTreePath) {
		return nil
	}
	if err := markRestoredModuleTree(staging, filepath.Base(moduleTreePath)); err != nil {
		return wrapFilesystemWriteError(staging, err)
	}
	if err := os.Rename(staging, moduleTreePath); err != nil {
		return wrapFilesystemWriteError(moduleTreePath, err)
	}
	cleanupStaging = false
	if err := chownRestoredModuleTreeFunc(moduleTreePath); err != nil {
		return wrapFilesystemWriteError(moduleTreePath, err)
	}
	return nil
}

func chownTreeToRoot(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Lchown(path, 0, 0)
	})
}

func currentRunningKernelVersion() string {
	out, err := exec.Command("uname", "-r").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func ensureEFIMirrorFromSnapshot(b *BootBackup) error {
	if !b.HasSnapshot {
		return fmt.Errorf("%w: %q", ErrBackupNotFound, b.Name)
	}
	if err := ensureEFIMountAvailable(); err != nil {
		return err
	}
	hadEFI := dirExists(b.EFIPath)
	if err := os.MkdirAll(b.EFIPath, 0o755); err != nil {
		return err
	}
	if err := syncDirContentsWithExcludes(b.SnapshotPath, b.EFIPath, []string{bootrecovMetadataRoot + "/**"}); err != nil {
		if !hadEFI {
			_ = os.RemoveAll(b.EFIPath)
		}
		return err
	}
	b.HasEFI = true
	b.InSync = efiMirrorHasBootArtifacts(*b)
	if !b.InSync {
		if !hadEFI {
			_ = os.RemoveAll(b.EFIPath)
		}
		return fmt.Errorf("%w: EFI mirror for %q is missing required boot artifacts after sync", ErrBackupIncomplete, b.Name)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return st.IsDir()
}

func firstExistingFile(base string, candidates []string) string {
	for _, name := range candidates {
		if fileExists(filepath.Join(base, name)) {
			return name
		}
	}
	return ""
}

func chooseMetadataPath(b *BootBackup) string {
	if b.HasSnapshot {
		return b.SnapshotPath
	}
	if b.HasEFI {
		return b.EFIPath
	}
	return ""
}

func findKernelAndInitramfs(base string) (string, string) {
	if base == "" {
		return "", ""
	}
	for _, pair := range [][2]string{
		{"vmlinuz-linux", "initramfs-linux.img"},
		{"vmlinuz", "initrd.img"},
	} {
		if fileExists(filepath.Join(base, pair[0])) && fileExists(filepath.Join(base, pair[1])) {
			return pair[0], pair[1]
		}
	}

	kernels := globBaseNames(base, "vmlinuz-*")
	initramfs := append(globBaseNames(base, "initrd.img-*"), globBaseNames(base, "initramfs-*.img")...)
	sort.Strings(initramfs)
	for _, kernel := range kernels {
		kernelVersion := parseKernelVersionFromName(kernel)
		if kernelVersion == "" {
			continue
		}
		for _, initrd := range initramfs {
			if parseKernelVersionFromName(initrd) == kernelVersion {
				return kernel, initrd
			}
		}
	}

	kernel := firstExistingFile(base, []string{"vmlinuz-linux", "vmlinuz"})
	if kernel == "" && len(kernels) > 0 {
		kernel = kernels[0]
	}
	initrd := firstExistingFile(base, []string{"initramfs-linux.img", "initrd.img"})
	if initrd == "" && len(initramfs) > 0 {
		initrd = initramfs[0]
	}
	return kernel, initrd
}

func globBaseNames(base, pattern string) []string {
	matches, _ := filepath.Glob(filepath.Join(base, pattern))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, filepath.Base(match))
	}
	sort.Strings(out)
	return out
}

func efiMirrorHasBootArtifacts(b BootBackup) bool {
	if !b.HasKernel || !b.HasInitramfs {
		return false
	}
	for _, name := range append(append([]string{}, b.MicrocodeImages...), b.KernelImage, b.InitramfsImage) {
		if !fileExists(filepath.Join(b.EFIPath, name)) {
			return false
		}
	}
	return true
}

func findMicrocodeImages(base string) []string {
	if base == "" {
		return nil
	}
	candidates := []string{"intel-ucode.img", "amd-ucode.img"}
	var out []string
	for _, c := range candidates {
		if fileExists(filepath.Join(base, c)) {
			out = append(out, c)
		}
	}
	return out
}

func detectKernelVersion(basePath, kernelImage, initramfsImage string) string {
	if kernelImage == "" && initramfsImage == "" {
		return ""
	}

	// Prefer explicit version from file names.
	for _, s := range []string{kernelImage, initramfsImage} {
		if v := parseKernelVersionFromName(s); v != "" {
			return v
		}
	}

	// Fallback: ask `file` for kernel version when available.
	if kernelImage != "" && basePath != "" {
		abs := filepath.Join(basePath, kernelImage)
		if v := parseKernelVersionFromFileCmd(abs); v != "" {
			return v
		}
	}
	return "unknown"
}

func parseKernelVersionFromName(name string) string {
	if name == "" {
		return ""
	}
	for _, prefix := range []string{"vmlinuz-", "initrd.img-", "initramfs-"} {
		if strings.HasPrefix(name, prefix) {
			version := strings.TrimPrefix(name, prefix)
			version = strings.TrimSuffix(version, ".img")
			version = strings.TrimSuffix(version, "-fallback")
			if regexp.MustCompile(`^\d+\.\d+`).MatchString(version) {
				return version
			}
			return ""
		}
	}
	// Examples: vmlinuz-6.8.0-31-generic, initrd.img-6.6.7-arch1-1
	re := regexp.MustCompile(`\d+\.\d+(\.\d+)?[-A-Za-z0-9._]*`)
	version := re.FindString(name)
	version = strings.TrimSuffix(version, ".img")
	version = strings.TrimSuffix(version, "-fallback")
	return version
}

func parseKernelVersionFromFileCmd(kernelPath string) string {
	if _, err := exec.LookPath("file"); err != nil {
		return ""
	}
	out, err := exec.Command("file", kernelPath).CombinedOutput()
	if err != nil {
		return ""
	}
	// Typical fragment: "version 6.8.0-31-generic (...)"
	s := string(out)
	idx := strings.Index(s, " version ")
	if idx == -1 {
		return ""
	}
	rest := s[idx+len(" version "):]
	end := strings.IndexAny(rest, ",(")
	if end == -1 {
		end = len(rest)
	}
	v := strings.TrimSpace(rest[:end])
	if v == "" {
		return ""
	}
	return v
}

func detectBackupTime(name string, paths ...string) time.Time {
	if t, ok := parseTimeFromBackupName(name); ok {
		return t
	}
	return latestModTime(paths...)
}

func parseTimeFromBackupName(name string) (time.Time, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return time.Time{}, false
	}
	formats := []string{
		"20060102-150405",
		"20060102-1504",
		"2006-01-02-150405",
		"2006-01-02-1504",
	}
	candidates := []string{name}
	if strings.HasPrefix(name, "snap-") {
		candidates = append(candidates, strings.TrimPrefix(name, "snap-"))
	}
	for _, c := range candidates {
		for _, f := range formats {
			if t, err := time.ParseInLocation(f, c, time.UTC); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func latestModTime(paths ...string) time.Time {
	var latest time.Time
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.ModTime().After(latest) {
			latest = st.ModTime()
		}
	}
	return latest
}

func dirSizeBytes(root string) int64 {
	return dirSizeBytesWithExcludes(root, nil)
}

func dirSizeBytesWithExcludes(root string, excludes []string) int64 {
	if root == "" {
		return 0
	}
	root = filepath.Clean(root)
	cleanExcludes := make([]string, 0, len(excludes))
	for _, ex := range excludes {
		cleanExcludes = append(cleanExcludes, filepath.Clean(ex))
	}
	var total int64
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if isExcludedPath(path, cleanExcludes) {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

func backupID(path string) string {
	sum := sha1.Sum([]byte(filepath.Clean(path)))
	return fmt.Sprintf("bootrecov-%x", sum[:6])
}

// DeleteBackup removes a backup by name from both mirror locations and
// removes the associated GRUB entry when present.
func DeleteBackup(name string) error {
	return withRecoveryOperation(func() error { return deleteBackup(name) })
}

func deleteBackup(name string) error {
	name = strings.TrimSpace(name)
	if err := validateBackupName(name); err != nil {
		return err
	}

	snapshotPath := filepath.Join(SnapshotDir, name)
	efiPath := filepath.Join(EfiDir, name)
	id := backupIDForName(name)
	exists, err := grubEntryExistsByID(id)
	if err != nil {
		return err
	}
	if exists {
		if err := removeGrubEntry(id); err != nil {
			return err
		}
	}
	if dirExists(efiPath) {
		if err := ensureEFIMountAvailable(); err != nil {
			return err
		}
		if err := os.RemoveAll(efiPath); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(snapshotPath); err != nil {
		return err
	}
	if err := cleanupRestoredModuleTrees(); err != nil {
		return fmt.Errorf("backup deleted; restored module cleanup incomplete: %w", err)
	}
	return nil
}

func checkSnapshotSpace() error {
	estimate := estimateBackupBytes()
	if estimate <= 0 {
		estimate = 64 * 1024 * 1024
	}
	snapshotFree, err := freeBytesAt(SnapshotDir)
	if err != nil {
		return fmt.Errorf("%w: unable to check free space for %s: %w", ErrFreeSpaceCheckFailed, SnapshotDir, err)
	}
	if snapshotFree < estimate {
		return fmt.Errorf(
			"%w: need %s, snapshot free=%s",
			ErrInsufficientSnapshotSpace,
			formatBytes(estimate),
			formatBytes(snapshotFree),
		)
	}
	return nil
}

func checkEFISpaceForSnapshot(snapshotPath string) error {
	size := dirSizeBytesWithExcludes(snapshotPath, []string{filepath.Join(snapshotPath, bootrecovMetadataRoot)})
	if size <= 0 {
		size = estimateBackupBytes()
	}
	need := int64(float64(size)*1.10) + 16*1024*1024
	efiFree, err := freeBytesAt(EfiDir)
	if err != nil {
		return fmt.Errorf("%w: unable to check free space for %s: %w", ErrFreeSpaceCheckFailed, EfiDir, err)
	}
	if efiFree < need {
		return fmt.Errorf(
			"%w to activate backup: need %s, free %s",
			ErrInsufficientEFISpace,
			formatBytes(need),
			formatBytes(efiFree),
		)
	}
	return nil
}

func ensureEFIMountAvailable() error {
	if !RequireEFIMount {
		return nil
	}
	mountRoot := filepath.Clean(filepath.Dir(EfiDir))
	mountPoint, err := findMountPoint(mountRoot)
	if err != nil {
		return &EFIMountError{MountRoot: mountRoot, Cause: err}
	}
	if filepath.Clean(mountPoint) != mountRoot {
		return &EFIMountError{MountRoot: mountRoot, MountPoint: mountPoint}
	}
	return nil
}

func estimateBackupBytes() int64 {
	size := dirSizeBytesWithExcludes(BootDir, bootTreeAbsoluteExcludes())
	size += estimateCurrentRootModuleBytes()
	if size <= 0 {
		return 64 * 1024 * 1024
	}
	// Add safety buffer for metadata/filesystem overhead.
	return int64(float64(size)*1.15) + 32*1024*1024
}

func estimateCurrentRootModuleBytes() int64 {
	kernel, initramfs := findKernelAndInitramfs(BootDir)
	version := detectKernelVersion(BootDir, kernel, initramfs)
	modulePath, known, exists := detectRootModuleTree(version)
	if !known || !exists {
		return 0
	}
	return dirSizeBytes(modulePath)
}

func freeBytesAt(path string) (int64, error) {
	target := existingParent(path)
	var st syscall.Statfs_t
	if err := statfsFunc(target, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

func existingParent(path string) string {
	p := filepath.Clean(path)
	for {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		next := filepath.Dir(p)
		if next == p {
			return p
		}
		p = next
	}
}

func formatBytes(bytes int64) string {
	const (
		ki = int64(1024)
		mi = ki * 1024
		gi = mi * 1024
	)
	switch {
	case bytes >= gi:
		return fmt.Sprintf("%.1fGiB", float64(bytes)/float64(gi))
	case bytes >= mi:
		return fmt.Sprintf("%.1fMiB", float64(bytes)/float64(mi))
	case bytes >= ki:
		return fmt.Sprintf("%.1fKiB", float64(bytes)/float64(ki))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func backupCapacitySummary() string {
	estimate := estimateBackupBytes()
	if estimate <= 0 {
		return "capacity: n/a"
	}
	snapFree, errSnap := freeBytesAt(SnapshotDir)
	efiFree, errEFI := freeBytesAt(EfiDir)
	if errSnap != nil || errEFI != nil {
		return "capacity: n/a"
	}
	count := maxBackupCountFromFree(efiFree, estimate)
	return fmt.Sprintf(
		"free: snap %s, efi %s | est: %s | ~%d active EFI mirrors",
		formatBytes(snapFree),
		formatBytes(efiFree),
		formatBytes(estimate),
		count,
	)
}

func maxBackupCountFromFree(freeBytes, estimate int64) int64 {
	if estimate <= 0 {
		return 0
	}
	return freeBytes / estimate
}

func copyBootSourceToSnapshot(snapshotTarget string) error {
	excludes := bootTreeRcloneExcludePatterns()
	if strings.EqualFold(strings.TrimSpace(BackupProfile), "minimal") {
		return syncDirContentsWithFilters(BootDir, snapshotTarget, excludes, minimalBootIncludePatterns())
	}
	return syncDirContentsWithExcludes(BootDir, snapshotTarget, excludes)
}

func bootTreeAbsoluteExcludes() []string {
	excludes := []string{
		filepath.Join(BootDir, "efi"),
		filepath.Join(BootDir, "efi", "bootrecov-snapshots"),
		filepath.Join(BootDir, "efi", "boot-backups"),
	}
	if rel, ok := cleanRelativeChild(BootDir, EfiDir); ok {
		excludes = append(excludes, filepath.Join(BootDir, rel))
	}
	return excludes
}

func bootTreeRcloneExcludePatterns() []string {
	excludes := []string{"efi/**", "efi/bootrecov-snapshots/**", "efi/boot-backups/**"}
	if rel, ok := cleanRelativeChild(BootDir, EfiDir); ok {
		excludes = append(excludes, filepath.ToSlash(rel)+"/**")
	}
	return uniqueStrings(excludes)
}

func cleanRelativeChild(root, child string) (string, bool) {
	root = filepath.Clean(root)
	child = filepath.Clean(child)
	if root == child {
		return "", false
	}
	rel, err := filepath.Rel(root, child)
	if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func minimalBootIncludePatterns() []string {
	return []string{
		"vmlinuz*",
		"initrd.img*",
		"initramfs*.img",
		"intel-ucode.img",
		"amd-ucode.img",
		"grub/**",
	}
}

// ActivateBackup copies a snapshot to EFI (after free-space check) and ensures
// a matching GRUB entry exists.
func ActivateBackup(name string) error {
	return withRecoveryOperation(func() error { return activateBackup(name) })
}

func activateBackup(name string) error {
	if err := ensureSupportedBootloader(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if err := validateBackupName(name); err != nil {
		return err
	}
	canonical := buildBackupFromName(name)
	refreshBackupCompleteness(&canonical)
	if !canonical.HasSnapshot {
		return fmt.Errorf("%w: %q", ErrBackupNotFound, name)
	}
	if !canonical.HasKernel || !canonical.HasInitramfs {
		return fmt.Errorf("%w: snapshot %q is incomplete", ErrBackupIncomplete, name)
	}
	if err := ensureEFIMountAvailable(); err != nil {
		return err
	}
	if !canonical.HasEFI {
		if err := checkEFISpaceForSnapshot(canonical.SnapshotPath); err != nil {
			return err
		}
	}
	if err := ensureRootModulesAvailable(&canonical); err != nil {
		return err
	}
	if !canonical.HasEFI {
		if err := ensureEFIMirrorFromSnapshot(&canonical); err != nil {
			return err
		}
	}
	return addGrubEntry(canonical)
}

// DeactivateBackup removes the GRUB entry and optional EFI mirror, while
// keeping the snapshot in SnapshotDir.
func DeactivateBackup(name string) error {
	return withRecoveryOperation(func() error { return deactivateBackup(name) })
}

func deactivateBackup(name string) error {
	if err := ensureSupportedBootloader(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if err := validateBackupName(name); err != nil {
		return err
	}
	if err := removeGrubEntry(backupIDForName(name)); err != nil {
		return err
	}
	if dirExists(filepath.Join(EfiDir, name)) {
		if err := ensureEFIMountAvailable(); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(filepath.Join(EfiDir, name)); err != nil {
		return err
	}
	if err := cleanupRestoredModuleTrees(); err != nil {
		return fmt.Errorf("backup deactivated; restored module cleanup incomplete: %w", err)
	}
	return nil
}

func syncDirContents(src, dst string) error {
	return syncDirContentsWithExcludes(src, dst, nil)
}

func syncDirContentsWithExcludes(src, dst string, excludes []string) error {
	return syncDirContentsWithFilters(src, dst, excludes, nil)
}

func syncDirContentsWithFilters(src, dst string, excludes, includes []string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	if src == dst {
		return nil
	}
	if !dirExists(src) {
		return fmt.Errorf("%w: %s", ErrSourceDirectoryMissing, src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return wrapFilesystemWriteError(dst, err)
	}

	if RcloneBin == "" {
		if RequireRclone {
			return fmt.Errorf("%w: rclone is required but not configured", ErrRequiredToolUnavailable)
		}
		return fallbackSyncCopy(src, dst, normalizeFallbackExcludes(src, excludes))
	}
	if _, err := exec.LookPath(RcloneBin); err != nil {
		if RequireRclone {
			return fmt.Errorf("%w: rclone is required for backup sync but was not found in PATH", ErrRequiredToolUnavailable)
		}
		return fallbackSyncCopy(src, dst, normalizeFallbackExcludes(src, excludes))
	}
	return runRcloneSync(src, dst, excludes, includes)
}

func runRcloneSync(src, dst string, excludes, includes []string) error {
	srcArg := src + string(os.PathSeparator)
	dstArg := dst + string(os.PathSeparator)
	args := buildRcloneSyncArgs(srcArg, dstArg, excludes, includes, detectSupportedRcloneSyncFlags())
	cmd := exec.Command(RcloneBin, args...)
	out, err := runCommandCombinedOutput(cmd)
	if err != nil {
		syncErr := fmt.Errorf("%w: rclone: %w: %s", ErrSyncFailed, err, strings.TrimSpace(string(out)))
		return wrapExternalWriteFailure(dst, syncErr)
	}
	return nil
}

func buildRcloneSyncArgs(srcArg, dstArg string, excludes, includes []string, supported map[string]bool) []string {
	args := []string{"sync", srcArg, dstArg}
	for _, flag := range []string{"--links", "--metadata", "--times", "--delete-during", "--perms"} {
		if supported[flag] {
			args = append(args, flag)
		}
	}
	for _, in := range includes {
		args = append(args, "--include", in)
	}
	for _, ex := range excludes {
		args = append(args, "--exclude", ex)
	}
	if len(includes) > 0 {
		args = append(args, "--exclude", "*")
	}
	return args
}

func detectSupportedRcloneSyncFlags() map[string]bool {
	// Conservative defaults: keep sync portable across older rclone builds.
	supported := map[string]bool{
		"--links":         false,
		"--metadata":      false,
		"--times":         false,
		"--delete-during": false,
		"--perms":         false,
	}

	cmd := exec.Command(RcloneBin, "sync", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return supported
	}
	help := string(out)
	for flag := range supported {
		if helpMentionsFlag(help, flag) {
			supported[flag] = true
		}
	}
	return supported
}

func helpMentionsFlag(help, flag string) bool {
	if flag == "" {
		return false
	}
	pattern := regexp.MustCompile(`(^|[\s,])` + regexp.QuoteMeta(flag) + `($|[\s,=])`)
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		if pattern.MatchString(trimmed) {
			return true
		}
	}
	return false
}

func fallbackSyncCopy(src, dst string, excludes []string) error {
	if err := clearDir(dst); err != nil {
		return err
	}
	return copyTree(src, dst, excludes)
}

func normalizeFallbackExcludes(src string, excludes []string) []string {
	out := make([]string, 0, len(excludes))
	for _, ex := range excludes {
		clean := strings.TrimSpace(ex)
		clean = strings.TrimPrefix(clean, "/")
		clean = strings.TrimSuffix(clean, "/**")
		clean = strings.TrimSuffix(clean, "/*")
		if clean == "" {
			continue
		}
		out = append(out, filepath.Join(src, clean))
	}
	return out
}

func clearDir(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(root, 0o755)
		}
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyTree(src, dst string, excludes []string) error {
	src = filepath.Clean(src)
	ex := make([]string, 0, len(excludes))
	for _, p := range excludes {
		ex = append(ex, filepath.Clean(p))
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == src {
			return nil
		}
		if isExcludedPath(path, ex) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.Type()&os.ModeSymlink != 0 {
			return copySymlink(path, target)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyRegularFile(path, target, info.Mode().Perm())
	})
}

func isExcludedPath(path string, excludes []string) bool {
	cleanPath := filepath.Clean(path)
	for _, ex := range excludes {
		if cleanPath == ex {
			return true
		}
		if strings.HasPrefix(cleanPath, ex+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func copyRegularFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return wrapFilesystemWriteError(dst, err)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return wrapFilesystemWriteError(dst, err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return wrapFilesystemWriteError(dst, err)
	}
	if err := out.Chmod(mode); err != nil {
		_ = out.Close()
		return err
	}
	return wrapFilesystemWriteError(dst, out.Close())
}

func copySymlink(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return wrapFilesystemWriteError(dst, err)
	}
	link, err := os.Readlink(src)
	if err != nil {
		return err
	}
	_ = os.Remove(dst)
	return wrapFilesystemWriteError(dst, os.Symlink(link, dst))
}

func wrapFilesystemWriteError(path string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ENOSPC) {
		return fmt.Errorf("%w: %w", insufficientSpaceErrorForPath(path), err)
	}
	return err
}

func wrapExternalWriteFailure(path string, err error) error {
	if err == nil {
		return nil
	}
	if isLowFreeSpaceAt(path) {
		return fmt.Errorf("%w: %w", insufficientSpaceErrorForPath(path), err)
	}
	return err
}

func insufficientSpaceErrorForPath(path string) error {
	clean := filepath.Clean(path)
	efiRoot := filepath.Clean(EfiDir)
	if clean == efiRoot || strings.HasPrefix(clean, efiRoot+string(os.PathSeparator)) {
		return ErrInsufficientEFISpace
	}
	return ErrInsufficientSnapshotSpace
}

func isLowFreeSpaceAt(path string) bool {
	free, err := freeBytesAt(path)
	return err == nil && free < lowFreeSpaceThreshold
}
