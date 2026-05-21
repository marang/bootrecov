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
	cmdline := blsKernelOptions(id)
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
	return GrubEntry{ID: id, BackupPath: backupPath, Name: filepath.Base(backupPath)}, true
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

func blsKernelOptions(id string) string {
	return strings.TrimSpace("$kernelopts " + kernelCmdlineMarker + id)
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
		fileExists(filepath.Join(DracutModuleDir, "bootrecov-restore.sh"))
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
	return nil
}

func fedoraDracutInstallPaths() []string {
	return []string{
		filepath.Join(DracutModuleDir, "module-setup.sh"),
		filepath.Join(DracutModuleDir, "bootrecov-restore.sh"),
	}
}

func renderDracutModuleSetup() string {
	return `#!/bin/sh

check() {
    require_binaries unsquashfs || return 1
    return 0
}

depends() {
    echo squash
    return 0
}

install() {
    inst_multiple unsquashfs mkdir mktemp mv rm chown cat uname
    inst_hook pre-pivot 95 "$moddir/bootrecov-restore.sh"
}
`
}

func renderDracutRestoreScript() string {
	rootModulesDir := filepath.Clean(RootModulesDir)
	if !filepath.IsAbs(rootModulesDir) {
		rootModulesDir = "/usr/lib/modules"
	}
	return fmt.Sprintf(`#!/bin/sh

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
    return 0 2>/dev/null || exit 0
}

case "${name}" in
    .|..|/*|*/*|*" "*) echo "bootrecov: refusing invalid snapshot name: ${name}" >&2; return 0 2>/dev/null || exit 0 ;;
esac

version="$(uname -r)"
sysroot="${NEWROOT:-/sysroot}"
root_modules_dir=%s
archive="${sysroot}/var/backups/bootrecov-snapshots/${name}/.bootrecov/root-modules/${version}.sqfs"
modules_parent="${sysroot}${root_modules_dir}"
target="${modules_parent}/${version}"

[ ! -d "${target}" ] || return 0 2>/dev/null || exit 0
[ -s "${archive}" ] || {
    echo "bootrecov: archived modules unavailable: ${archive}" >&2
    return 0 2>/dev/null || exit 0
}

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
    rm -rf "${staging}"
    return 0 2>/dev/null || exit 0
fi

if [ -d "${target}" ]; then
    rm -rf "${staging}"
    return 0 2>/dev/null || exit 0
fi

if mv "${staging}" "${target}"; then
    chown -R 0:0 "${target}" 2>/dev/null || true
    echo "bootrecov: restored modules for ${version} from ${name}" >&2
else
    echo "bootrecov: failed to move restored modules into ${target}" >&2
    rm -rf "${staging}"
fi
`, shellSingleQuote(rootModulesDir))
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
