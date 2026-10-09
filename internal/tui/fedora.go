package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const blsBootrecovTitlePrefix = "Bootrecov "

var fedoraBootCriticalPackageFilters = []string{
	"kernel*",
	"grub*",
	"shim*",
	"dracut*",
	"systemd*",
	"microcode_ctl",
}

func addBLSEntry(b BootBackup) error {
	if !canUseBLSForBackup(b) {
		return fmt.Errorf("%w: BLS entries are unavailable for snapshot %q", ErrUnsupportedBootloader, b.Name)
	}
	id := backupIDForName(b.Name)
	path := blsEntryPath(id)
	if fileExists(path) {
		return nil
	}
	cmdline := blsKernelOptions(id, b.Name)
	linuxPath := blsVisiblePath(filepath.Join(b.EFIPath, b.KernelImage))
	initrdPaths := make([]string, 0, len(b.MicrocodeImages)+1)
	for _, microcode := range b.MicrocodeImages {
		initrdPaths = append(initrdPaths, blsVisiblePath(filepath.Join(b.EFIPath, microcode)))
	}
	initrdPaths = append(initrdPaths, blsVisiblePath(filepath.Join(b.EFIPath, b.InitramfsImage)))
	data := renderBLSEntry(id, b, linuxPath, initrdPaths, cmdline)
	if err := os.MkdirAll(BLSEntriesDir, 0o755); err != nil {
		return wrapFilesystemWriteError(BLSEntriesDir, err)
	}
	return os.WriteFile(path, []byte(data), 0o644)
}

func removeBLSEntry(id string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	if err := os.Remove(blsEntryPath(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func listBLSEntries() ([]GrubEntry, error) {
	entries, err := os.ReadDir(BLSEntriesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []GrubEntry{}, nil
		}
		return nil, err
	}
	var out []GrubEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".conf")
		if !strings.HasPrefix(id, "bootrecov-") {
			continue
		}
		parsed, ok := parseBLSEntryFile(id, filepath.Join(BLSEntriesDir, entry.Name()))
		if ok {
			out = append(out, parsed)
		}
	}
	return out, nil
}

func parseBLSEntryFile(id, path string) (GrubEntry, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GrubEntry{}, false
	}
	values := parseSimpleKeyValues(string(data))
	title := strings.TrimSpace(values["title"])
	if !strings.HasPrefix(title, blsBootrecovTitlePrefix) {
		return GrubEntry{}, false
	}
	backupPath := strings.TrimSpace(strings.TrimPrefix(title, blsBootrecovTitlePrefix))
	if backupPath == "" {
		return GrubEntry{}, false
	}
	if entryID := strings.TrimSpace(values["id"]); entryID != "" {
		id = entryID
	}
	if id == "" || !strings.HasPrefix(id, "bootrecov-") {
		return GrubEntry{}, false
	}
	return GrubEntry{ID: id, BackupPath: backupPath, Name: filepath.Base(backupPath), isBLS: true}, true
}

func parseSimpleKeyValues(data string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}

func renderBLSEntry(id string, b BootBackup, linuxPath string, initrdPaths []string, options string) string {
	version := strings.TrimSpace(b.KernelVersion)
	if version == "" || version == "unknown" {
		version = b.Name
	}
	var lines []string
	lines = append(lines,
		"title "+blsBootrecovTitlePrefix+b.EFIPath,
		"version "+version,
		"linux "+linuxPath,
	)
	for _, initrd := range initrdPaths {
		lines = append(lines, "initrd "+initrd)
	}
	lines = append(lines,
		"options "+options,
		"id "+id,
		"",
	)
	return strings.Join(lines, "\n")
}

func blsKernelOptions(id, name string) string {
	return strings.TrimSpace("$kernelopts " + kernelCmdlineMarker + id + " bootrecov_snapshot=" + name)
}

func blsEntryPath(id string) string {
	return filepath.Join(BLSEntriesDir, id+".conf")
}

func canUseBLSForBackup(b BootBackup) bool {
	if currentPlatformID() != PlatformFedora || !dirExists(BLSEntriesDir) {
		return false
	}
	return sameMountRoot(BLSEntriesDir, b.EFIPath)
}

func sameMountRoot(a, b string) bool {
	aMount, aErr := findMountPoint(filepath.Clean(a))
	bMount, bErr := findMountPoint(filepath.Clean(b))
	if aErr == nil && bErr == nil && aMount != "" && bMount != "" {
		return filepath.Clean(aMount) == filepath.Clean(bMount)
	}
	return true
}

func blsVisiblePath(hostPath string) string {
	return grubVisiblePath(hostPath)
}

func installFedoraHooks(executablePath string) (err error) {
	if err := ensurePlatformHookSupported(); err != nil {
		return err
	}
	if err := ensureInitramfsHookSupported(); err != nil {
		return err
	}
	executablePath, err = validateHookExecutablePath(executablePath)
	if err != nil {
		return err
	}
	paths := []string{DNF5ActionsPath, DNF4PreActionsPath, DNF4PostActionsPath}
	paths = append(paths, fedoraDracutInstallPaths()...)
	snapshots, err := snapshotInstallPaths(paths)
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
	wroteDNF, err := installFedoraDNFActions(executablePath)
	if err != nil {
		return err
	}
	if !wroteDNF {
		return fmt.Errorf("%w: no DNF action plugin directory found; install libdnf5-plugin-actions or dnf-plugins-core pre/post transaction actions", ErrUnsupportedPackageHook)
	}
	if err := installFedoraDracutModule(); err != nil {
		return err
	}
	if err := regenerateDracutInitramfs(); err != nil {
		return err
	}
	installed = true
	return nil
}

func uninstallFedoraHooks() (bool, error) {
	removed := false
	for _, path := range []string{DNF5ActionsPath, DNF4PreActionsPath, DNF4PostActionsPath} {
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, err
		}
		removed = true
	}
	if dirExists(DracutModuleDir) {
		if err := os.RemoveAll(DracutModuleDir); err != nil {
			return removed, err
		}
		removed = true
	}
	if err := os.Remove(DracutConfigPath); err != nil {
		if !os.IsNotExist(err) {
			return removed, err
		}
	} else {
		removed = true
	}
	if removed {
		if err := regenerateDracutInitramfs(); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func fedoraHooksInstalled() bool {
	return (fileExists(DNF5ActionsPath) || (fileExists(DNF4PreActionsPath) && fileExists(DNF4PostActionsPath))) &&
		fileExists(filepath.Join(DracutModuleDir, "module-setup.sh")) &&
		fileExists(filepath.Join(DracutModuleDir, "bootrecov-restore.sh")) &&
		fileExists(DracutConfigPath)
}

func installFedoraDNFActions(executablePath string) (bool, error) {
	if dirExists(filepath.Dir(DNF5ActionsPath)) {
		if err := os.WriteFile(DNF5ActionsPath, []byte(renderDNF5Actions(executablePath)), 0o644); err != nil {
			return false, err
		}
		return true, nil
	}
	preDir := filepath.Dir(DNF4PreActionsPath)
	postDir := filepath.Dir(DNF4PostActionsPath)
	if dirExists(preDir) && dirExists(postDir) {
		if err := os.WriteFile(DNF4PreActionsPath, []byte(renderDNF4PreActions(executablePath)), 0o644); err != nil {
			return false, err
		}
		if err := os.WriteFile(DNF4PostActionsPath, []byte(renderDNF4PostActions(executablePath)), 0o644); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func renderDNF5Actions(executablePath string) string {
	var lines []string
	lines = append(lines, "# Created by bootrecov. Requires libdnf5-plugin-actions.")
	for _, filter := range fedoraBootCriticalPackageFilters {
		lines = append(lines, fmt.Sprintf("pre_transaction:%s:::/usr/bin/env BOOTRECOV_ACCEPT_RISK=1 %s hook backup-now", filter, executablePath))
		lines = append(lines, fmt.Sprintf("post_transaction:%s:::/usr/bin/env BOOTRECOV_ACCEPT_RISK=1 %s hook reconcile-active", filter, executablePath))
	}
	return strings.Join(lines, "\n") + "\n"
}

func renderDNF4PreActions(executablePath string) string {
	return renderDNF4Actions("Created by bootrecov. Requires python3-dnf-plugin-pre-transaction-actions.", executablePath, "backup-now")
}

func renderDNF4PostActions(executablePath string) string {
	return renderDNF4Actions("Created by bootrecov. Requires python3-dnf-plugin-post-transaction-actions.", executablePath, "reconcile-active")
}

func renderDNF4Actions(comment string, executablePath string, hookCommand string) string {
	var lines []string
	lines = append(lines, "# "+comment)
	for _, filter := range fedoraBootCriticalPackageFilters {
		lines = append(lines, fmt.Sprintf("%s:any:/usr/bin/env BOOTRECOV_ACCEPT_RISK=1 %s hook %s", filter, executablePath, hookCommand))
	}
	return strings.Join(lines, "\n") + "\n"
}

func installFedoraDracutModule() error {
	if err := os.MkdirAll(DracutModuleDir, 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"module-setup.sh":      renderDracutModuleSetup(),
		"bootrecov-restore.sh": renderDracutRestoreScript(),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(DracutModuleDir, name), []byte(data), 0o755); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(DracutConfigPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(DracutConfigPath, []byte("# Created by bootrecov. Include the restore hook in host-only images.\nforce_add_dracutmodules+=\" bootrecov \"\n"), 0o644); err != nil {
		return err
	}
	return nil
}

func fedoraDracutInstallPaths() []string {
	return []string{
		filepath.Join(DracutModuleDir, "module-setup.sh"),
		filepath.Join(DracutModuleDir, "bootrecov-restore.sh"),
		DracutConfigPath,
	}
}

func renderDracutModuleSetup() string {
	return `#!/bin/sh

check() {
    require_binaries unsquashfs || return 1
    return 0
}

install() {
    inst_multiple unsquashfs mkdir mktemp mv rm chown cat uname stat mount umount mountpoint rmdir findmnt
    inst_hook pre-pivot 95 "$moddir/bootrecov-restore.sh"
}
`
}

func renderDracutRestoreScript() string {
	rootModulesDir := filepath.Clean(RootModulesDir)
	if !filepath.IsAbs(rootModulesDir) {
		rootModulesDir = "/usr/lib/modules"
	}
	mirrorRoot := filepath.Clean(EfiDir)
	if !filepath.IsAbs(mirrorRoot) {
		mirrorRoot = ""
	}
	mirrorVisible := grubVisiblePath(EfiDir)
	if !strings.HasPrefix(mirrorVisible, "/") || mirrorVisible == "/" {
		mirrorVisible = ""
	}
	return fmt.Sprintf(`#!/bin/sh

mirror_root=%s
mirror_visible=%s
cmdline="$(cat /proc/cmdline 2>/dev/null || true)"
has_marker=0
entry_count=0
entry_id=""
image_count=0
image_path=""
marker_count=0
marker_name=""
name=""
for entry in ${cmdline}; do
    case "${entry}" in
        BOOT_IMAGE=*) image_count=$((image_count + 1)); image_path="${entry#BOOT_IMAGE=}" ;;
    esac
    case "${entry}" in
        bootrecov_entry=*)
            has_marker=1
            entry_count=$((entry_count + 1))
            entry_id="${entry#bootrecov_entry=}"
            ;;
        bootrecov_snapshot=*)
            marker_count=$((marker_count + 1))
            marker_name="${entry#bootrecov_snapshot=}"
            ;;
        BOOT_IMAGE=/bootrecov-snapshots/*)
            boot_image="${entry#BOOT_IMAGE=/bootrecov-snapshots/}"
            case "${boot_image}" in */*/*) ;; */*) name="${boot_image%%/*}" ;; esac
            ;;
        BOOT_IMAGE=*/bootrecov-snapshots/*)
            boot_image="${entry#*/bootrecov-snapshots/}"
            case "${boot_image}" in */*/*) ;; */*) name="${boot_image%%/*}" ;; esac
            ;;
    esac
done

if [ "${entry_count}" -gt 1 ]; then
    echo "bootrecov: ambiguous Bootrecov entry marker" >&2
    return 0 2>/dev/null || exit 0
fi
if [ "${entry_count}" -eq 1 ]; then
    case "${entry_id}" in bootrecov-*) entry_suffix="${entry_id#bootrecov-}" ;; *) entry_suffix="" ;; esac
    case "${entry_suffix}" in
        *[!0-9a-f]*|"") echo "bootrecov: invalid Bootrecov entry marker" >&2; return 0 2>/dev/null || exit 0 ;;
    esac
    if [ "${#entry_suffix}" -ne 12 ]; then
        echo "bootrecov: invalid Bootrecov entry marker" >&2
        return 0 2>/dev/null || exit 0
    fi
fi

if [ "${image_count}" -gt 1 ]; then
    echo "bootrecov: ambiguous snapshot identity: multiple BOOT_IMAGE values" >&2
    return 0 2>/dev/null || exit 0
fi
case "${image_path}" in
    *"//"*|*/./*|*/../*|*/.|*/..|*/)
        echo "bootrecov: invalid BOOT_IMAGE path" >&2
        return 0 2>/dev/null || exit 0
        ;;
esac
image_name=""
case "${image_path}" in
    */*/*)
        boot_image="${image_path%%/*}"
        image_name="${boot_image##*/}"
        ;;
esac

if [ "${entry_count}" -eq 0 ] && [ "${marker_count}" -eq 0 ] && [ -z "${name}" ] && [ -n "${image_path}" ]; then
    image_visible="/${image_path#*/}"
    for mirror in "${mirror_root}" "${mirror_visible}"; do
        case "${mirror}" in ""|/) continue ;; esac
        case "${image_visible}" in
            "${mirror}"/*)
                mirror_tail="${image_visible#"${mirror}"/}"
                case "${mirror_tail}" in
                    */*/*|""|/*) echo "bootrecov: invalid BOOT_IMAGE path under configured mirror" >&2; return 0 2>/dev/null || exit 0 ;;
                    */*) name="${mirror_tail%%/*}" ;;
                esac
                break
                ;;
        esac
    done
fi

if [ "${marker_count}" -gt 1 ]; then
    echo "bootrecov: ambiguous snapshot identity: multiple snapshot markers" >&2
    return 0 2>/dev/null || exit 0
fi
if [ "${marker_count}" -eq 1 ] && [ -z "${marker_name}" ]; then
    echo "bootrecov: refusing invalid snapshot name: empty snapshot marker" >&2
    return 0 2>/dev/null || exit 0
fi
if [ "${marker_count}" -eq 1 ]; then
    case "${marker_name}" in
        [!A-Za-z0-9]*|*[!A-Za-z0-9._-]*) echo "bootrecov: refusing invalid snapshot name: ${marker_name}" >&2; return 0 2>/dev/null || exit 0 ;;
    esac
fi
if [ "${marker_count}" -eq 1 ] && [ "${has_marker}" -eq 0 ]; then
    echo "bootrecov: snapshot parameter found without Bootrecov entry marker" >&2
    return 0 2>/dev/null || exit 0
fi

if [ "${marker_count}" -eq 0 ] && [ "${has_marker}" -eq 1 ] && [ -n "${image_name}" ]; then
    name="${image_name}"
fi

if [ -n "${marker_name}" ] && [ -n "${image_name}" ] && [ "${marker_name}" != "${image_name}" ]; then
    echo "bootrecov: conflicting snapshot identities in boot parameters" >&2
    return 0 2>/dev/null || exit 0
fi
[ -z "${marker_name}" ] || name="${marker_name}"

[ -n "${name}" ] || {
    [ "${has_marker}" -eq 0 ] || echo "bootrecov: fallback marker found but snapshot name could not be parsed" >&2
    return 0 2>/dev/null || exit 0
}

case "${name}" in
    [!A-Za-z0-9]*|*[!A-Za-z0-9._-]*) echo "bootrecov: refusing invalid snapshot name: ${name}" >&2; return 0 2>/dev/null || exit 0 ;;
esac

(
version="$(uname -r)"
sysroot="${NEWROOT:-/sysroot}"
root_modules_dir=%s
modules_parent="${sysroot}${root_modules_dir}"
target="${modules_parent}/${version}"
[ ! -d "${target}" ] || return 0 2>/dev/null || exit 0
archive_root="${sysroot}/var"
var_mount=""
var_mounted=0
target_mount=""
target_readonly=0
staging=""
cleanup_var_mount() {
    [ -n "${var_mount}" ] || return 0
    if [ "${var_mounted}" -eq 1 ]; then
        if ! umount "${var_mount}"; then
            echo "bootrecov: cannot unmount temporary /var archive mount" >&2
            return 1
        fi
        var_mounted=0
    fi
    rmdir "${var_mount}" 2>/dev/null || true
    var_mount=""
}
cleanup_restore_mounts() {
    cleanup_failed=0
    [ -z "${staging}" ] || rm -rf "${staging}"
    cleanup_var_mount || cleanup_failed=1
    if [ "${target_readonly}" -eq 1 ]; then
        if ! mount -o remount,ro -- "${target_mount}"; then
            echo "bootrecov: cannot restore read-only mode of ${target_mount}" >&2
            cleanup_failed=1
        fi
    fi
    return "${cleanup_failed}"
}
trap 'cleanup_restore_mounts || exit 1' EXIT
var_count=0
var_spec=""
var_fs=""
var_opts=""
if [ -r "${sysroot}/etc/fstab" ]; then
    while read -r spec mount_path fs opts rest || [ -n "${spec}" ]; do
        case "${spec}" in ""|\#*) continue ;; esac
        [ "${mount_path}" = /var ] || continue
        var_count=$((var_count + 1))
        var_spec="${spec}"
        var_fs="${fs}"
        var_opts="${opts}"
    done < "${sysroot}/etc/fstab"
fi
if [ "${var_count}" -gt 1 ]; then
    echo "bootrecov: ambiguous /var mount in fstab; cannot locate module archive" >&2
    return 0 2>/dev/null || exit 0
fi
if [ "${var_count}" -eq 1 ] && ! mountpoint -q "${sysroot}/var"; then
    case "${var_spec}" in /dev/*|UUID=*|LABEL=*|PARTUUID=*|PARTLABEL=*) ;; *) echo "bootrecov: unsupported /var mount source in fstab" >&2; return 0 2>/dev/null || exit 0 ;; esac
    case "${var_fs}" in btrfs|ext4|xfs) ;; *) echo "bootrecov: unsupported /var filesystem in fstab: ${var_fs}" >&2; return 0 2>/dev/null || exit 0 ;; esac
    case "${var_opts}" in ""|-) var_opts=defaults ;; esac
    var_mount="$(mktemp -d "${TMPDIR:-/run}/bootrecov-var.XXXXXX" 2>/dev/null)" || {
        echo "bootrecov: cannot create temporary /var mountpoint" >&2
        return 0 2>/dev/null || exit 0
    }
    if ! mount -t "${var_fs}" -o "${var_opts},ro" "${var_spec}" "${var_mount}"; then
        echo "bootrecov: cannot mount separate /var to locate module archive" >&2
        return 0 2>/dev/null || exit 0
    fi
    var_mounted=1
    archive_root="${var_mount}"
fi
archive="${archive_root}/backups/bootrecov-snapshots/${name}/.bootrecov/root-modules/${version}.sqfs"

[ -s "${archive}" ] || {
    echo "bootrecov: archived modules unavailable: ${archive}" >&2
    return 0 2>/dev/null || exit 0
}

mount_probe="${modules_parent}"
while [ ! -e "${mount_probe}" ] && [ "${mount_probe}" != "${sysroot}" ]; do
    mount_probe="${mount_probe%%/*}"
done
target_mount="$(findmnt -n -o TARGET -T "${mount_probe}" 2>/dev/null)" || {
    echo "bootrecov: cannot identify module restore target mount" >&2
    return 0 2>/dev/null || exit 0
}
case "${target_mount}" in
    "${sysroot}"|"${sysroot}"/*) ;;
    *) echo "bootrecov: module restore target mount lies outside the real root" >&2; return 0 2>/dev/null || exit 0 ;;
esac
target_options="$(findmnt -n -o VFS-OPTIONS -T "${mount_probe}" 2>/dev/null)" || {
    echo "bootrecov: cannot identify module restore target mount mode" >&2
    return 0 2>/dev/null || exit 0
}
case ",${target_options}," in
    *,ro,*)
        target_readonly=1
        if ! mount -o remount,rw -- "${target_mount}"; then
            echo "bootrecov: cannot make module restore target mount writable: ${target_mount}" >&2
            return 0 2>/dev/null || exit 0
        fi
        ;;
    *,rw,*) ;;
    *) echo "bootrecov: unknown module restore target mount mode" >&2; return 0 2>/dev/null || exit 0 ;;
esac

mkdir -p "${modules_parent}" || {
    echo "bootrecov: cannot create ${modules_parent}" >&2
    return 0 2>/dev/null || exit 0
}

staging="$(mktemp -d "${modules_parent}/.bootrecov-restore.XXXXXX" 2>/dev/null)" || {
    echo "bootrecov: cannot create module restore staging directory" >&2
    return 0 2>/dev/null || exit 0
}

if ! unsquashfs -d "${staging}" "${archive}" >/dev/null 2>&1; then
    echo "bootrecov: failed to restore modules from ${archive}" >&2
    return 0 2>/dev/null || exit 0
fi
if ! cleanup_var_mount; then
    return 0 2>/dev/null || exit 0
fi

if [ -d "${target}" ]; then
    rm -rf "${staging}"
    return 0 2>/dev/null || exit 0
fi

inode="$(stat -c '%%i' "${staging}" 2>/dev/null)"
case "${inode}" in
    ''|*[!0-9]*) echo "bootrecov: cannot identify restored module directory" >&2; rm -rf "${staging}"; return 0 2>/dev/null || exit 0 ;;
esac
if ! rm -f "${staging}/%s" || ! printf '%%s\n%%s\n' "${version}" "${inode}" > "${staging}/%s"; then
    echo "bootrecov: cannot mark restored modules" >&2
    rm -rf "${staging}"
    return 0 2>/dev/null || exit 0
fi

if mv "${staging}" "${target}"; then
    staging=""
    chown -R 0:0 "${target}" 2>/dev/null || true
    echo "bootrecov: restored modules for ${version} from ${name}" >&2
else
    echo "bootrecov: failed to move restored modules into ${target}" >&2
    rm -rf "${staging}"
fi
)
`, shellSingleQuote(mirrorRoot), shellSingleQuote(mirrorVisible), shellSingleQuote(rootModulesDir), restoredModuleMarker, restoredModuleMarker)
}

func regenerateDracutInitramfs() error {
	if !UpdateInitramfs {
		return nil
	}
	if strings.TrimSpace(DracutBin) == "" {
		return fmt.Errorf("%w: dracut is required but not configured", ErrRequiredToolUnavailable)
	}
	if _, err := exec.LookPath(DracutBin); err != nil {
		return fmt.Errorf("%w: dracut is required for initramfs regeneration but was not found in PATH", ErrRequiredToolUnavailable)
	}
	cmd := exec.Command(DracutBin, "--regenerate-all", "--force")
	out, err := runCommandCombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("%w: dracut --regenerate-all --force: %w: %s", ErrCommandFailed, err, strings.TrimSpace(string(out)))
	}
	return nil
}
