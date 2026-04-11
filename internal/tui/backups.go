package tui

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// BootBackup represents one logical backup snapshot stored in SnapshotDir.
// An optional EFI mirror exists in EfiDir only when the snapshot is activated
// for GRUB booting.
type BootBackup struct {
	Name            string
	Path            string // Canonical path used for GRUB entries (EFI copy)
	SnapshotPath    string
	EFIPath         string
	MetadataPath    string
	HasSnapshot     bool
	HasEFI          bool
	InSync          bool
	KernelImage     string
	InitramfsImage  string
	MicrocodeImages []string
	KernelVersion   string
	CreatedAt       time.Time
	SizeBytes       int64
	HasKernel       bool
	HasInitramfs    bool
	GrubEntryExists bool
}

type GrubEntry struct {
	ID         string
	BackupPath string
	Name       string
}

var (
	BootDir        = "/boot"
	SnapshotDir    = "/var/backups/bootrecov-snapshots"
	EfiDir         = "/boot/efi/bootrecov-snapshots"
	GrubCustom     = "/etc/grub.d/41_bootrecov_snapshots"
	GrubCfgOutput  = "/boot/grub/grub.cfg"
	GrubMkconfig   = "grub-mkconfig"
	AutoUpdateGrub = true
	PacmanHookPath = "/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook"
	RcloneBin      = "rclone"
	RequireRclone  = true
	BackupProfile  = "full" // full|minimal
	grubHeader     = "#!/bin/bash\n"
	statfsFunc     = syscall.Statfs
	mountInfoPath  = "/proc/self/mountinfo"
)

const (
	backupNameCurrentDir = "."
	backupNameParentDir  = ".."
	kernelCmdlineMarker  = "bootrecov_entry="
)

// CreateBootBackupNow copies the current /boot tree to SnapshotDir.
// EFI mirrors are created only when explicitly activated. The backup name is a
// UTC timestamp.
func CreateBootBackupNow() (BootBackup, error) {
	if err := checkSnapshotSpace(); err != nil {
		return BootBackup{}, err
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	snapshotTarget := filepath.Join(SnapshotDir, stamp)

	if err := os.MkdirAll(snapshotTarget, 0o755); err != nil {
		return BootBackup{}, err
	}

	if err := copyBootSourceToSnapshot(snapshotTarget); err != nil {
		return BootBackup{}, err
	}
	created := buildBackupFromName(stamp)
	refreshBackupCompleteness(&created)
	return created, nil
}

func InstallPacmanHook(executablePath string) error {
	if strings.TrimSpace(executablePath) == "" {
		executablePath = defaultHookExecutablePath()
	}
	executablePath = filepath.Clean(strings.TrimSpace(executablePath))
	if !filepath.IsAbs(executablePath) {
		return fmt.Errorf("hook executable path must be absolute: %s", executablePath)
	}
	if err := os.MkdirAll(filepath.Dir(PacmanHookPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(PacmanHookPath, []byte(renderPacmanHook(executablePath)), 0o644)
}

func renderPacmanHook(executablePath string) string {
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
Exec = %s backup-now
`, executablePath)
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
	if RequireRclone {
		if strings.TrimSpace(RcloneBin) == "" {
			missing = append(missing, "rclone (not configured)")
		} else if _, err := exec.LookPath(RcloneBin); err != nil {
			missing = append(missing, fmt.Sprintf("%s (required for snapshot and EFI sync)", RcloneBin))
		}
	}
	if AutoUpdateGrub {
		if strings.TrimSpace(GrubMkconfig) == "" {
			missing = append(missing, "grub-mkconfig (not configured)")
		} else if _, err := exec.LookPath(GrubMkconfig); err != nil {
			missing = append(missing, fmt.Sprintf("%s (required to regenerate %s)", GrubMkconfig, GrubCfgOutput))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("bootrecov cannot start because required dependencies are missing:\n- %s", strings.Join(missing, "\n- "))
}

// RefreshBackupsAndGrub loads current backups and GRUB entries without
// modifying backup directories or GRUB content.
func RefreshBackupsAndGrub() ([]BootBackup, []GrubEntry, error) {
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

// SyncBackupsAndGrub reconciles optional EFI mirrors used by GRUB entries:
// - keeps EFI mirrors only for activated snapshots
// - refreshes active EFI mirrors from snapshot source
// - removes stale GRUB entries
func SyncBackupsAndGrub() ([]BootBackup, []GrubEntry, error) {
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
	preserveGrubForName := map[string]struct{}{}
	for i := range backups {
		b := &backups[i]
		if !b.HasSnapshot && b.HasEFI {
			if rmErr := os.RemoveAll(b.EFIPath); rmErr != nil {
				b.InSync = false
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
				b.InSync = false
			} else {
				b.HasEFI = false
			}
			refreshBackupCompleteness(b)
			continue
		}
		wasBootable := b.HasSnapshot && b.HasEFI && b.HasKernel && b.HasInitramfs
		if isActivated {
			if err := ensureEFIMirrorFromSnapshot(b); err != nil {
				b.InSync = false
				if wasBootable {
					preserveGrubForName[b.Name] = struct{}{}
				}
			}
		}
		refreshBackupCompleteness(b)
	}
	if err := removeStaleGrubEntries(backups, preserveGrubForName); err != nil {
		return nil, nil, err
	}
	entries, err = ListGrubEntries()
	if err != nil {
		return nil, nil, err
	}
	markGrubFlags(backups, entries)
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
			if e.IsDir() {
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

func buildBackupFromName(name string) BootBackup {
	snapshotPath := filepath.Join(SnapshotDir, name)
	efiPath := filepath.Join(EfiDir, name)
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
	b.CreatedAt = detectBackupTime(b.Name, b.SnapshotPath, b.EFIPath)
	b.SizeBytes = dirSizeBytes(b.MetadataPath)
	b.HasKernel = b.KernelImage != ""
	b.HasInitramfs = b.InitramfsImage != ""
}

func ensureEFIMirrorFromSnapshot(b *BootBackup) error {
	if !b.HasSnapshot {
		return fmt.Errorf("snapshot missing for %q", b.Name)
	}
	if err := os.MkdirAll(b.EFIPath, 0o755); err != nil {
		return err
	}
	if err := syncDirContents(b.SnapshotPath, b.EFIPath); err != nil {
		return err
	}
	b.HasEFI = true
	b.InSync = true
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
	kernel := firstExistingFile(base, []string{"vmlinuz-linux", "vmlinuz"})
	initramfs := firstExistingFile(base, []string{"initramfs-linux.img", "initrd.img"})
	if kernel == "" {
		if matches, _ := filepath.Glob(filepath.Join(base, "vmlinuz-*")); len(matches) > 0 {
			kernel = filepath.Base(matches[0])
		}
	}
	if initramfs == "" {
		patterns := []string{"initrd.img-*", "initramfs-*.img"}
		for _, p := range patterns {
			matches, _ := filepath.Glob(filepath.Join(base, p))
			if len(matches) > 0 {
				initramfs = filepath.Base(matches[0])
				break
			}
		}
	}
	return kernel, initramfs
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
	// Examples: vmlinuz-6.8.0-31-generic, initrd.img-6.6.7-arch1-1
	re := regexp.MustCompile(`\d+\.\d+(\.\d+)?[-A-Za-z0-9._]*`)
	return re.FindString(name)
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
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("invalid backup name")
	}
	if strings.ContainsRune(name, '/') || name == backupNameCurrentDir || name == backupNameParentDir {
		return fmt.Errorf("invalid backup name: %q", name)
	}

	snapshotPath := filepath.Join(SnapshotDir, name)
	efiPath := filepath.Join(EfiDir, name)
	if err := os.RemoveAll(snapshotPath); err != nil {
		return err
	}
	if err := os.RemoveAll(efiPath); err != nil {
		return err
	}

	id := backupIDForName(name)
	exists, err := grubEntryExistsByID(id)
	if err != nil {
		return err
	}
	if exists {
		if err := RemoveGrubEntry(id); err != nil {
			return err
		}
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
		return fmt.Errorf("unable to check free space for %s: %w", SnapshotDir, err)
	}
	if snapshotFree < estimate {
		return fmt.Errorf(
			"insufficient free space (need %s): snapshot free=%s",
			formatBytes(estimate),
			formatBytes(snapshotFree),
		)
	}
	return nil
}

func checkEFISpaceForSnapshot(snapshotPath string) error {
	size := dirSizeBytes(snapshotPath)
	if size <= 0 {
		size = estimateBackupBytes()
	}
	need := int64(float64(size)*1.10) + 16*1024*1024
	efiFree, err := freeBytesAt(EfiDir)
	if err != nil {
		return fmt.Errorf("unable to check free space for %s: %w", EfiDir, err)
	}
	if efiFree < need {
		return fmt.Errorf(
			"insufficient EFI free space to activate backup (need %s, free %s)",
			formatBytes(need),
			formatBytes(efiFree),
		)
	}
	return nil
}

func estimateBackupBytes() int64 {
	excludes := []string{
		filepath.Join(BootDir, "efi", "bootrecov-snapshots"),
		filepath.Join(BootDir, "efi", "boot-backups"),
	}
	size := dirSizeBytesWithExcludes(BootDir, excludes)
	if size <= 0 {
		return 64 * 1024 * 1024
	}
	// Add safety buffer for metadata/filesystem overhead.
	return int64(float64(size)*1.15) + 32*1024*1024
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
	excludes := []string{"efi/bootrecov-snapshots/**", "efi/boot-backups/**"}
	if strings.EqualFold(strings.TrimSpace(BackupProfile), "minimal") {
		return syncDirContentsWithFilters(BootDir, snapshotTarget, excludes, minimalBootIncludePatterns())
	}
	return syncDirContentsWithExcludes(BootDir, snapshotTarget, excludes)
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

func grubInitrdArgs(backupPath string, microcodes []string, initramfs string) string {
	args := make([]string, 0, len(microcodes)+1)
	for _, mc := range microcodes {
		args = append(args, filepath.ToSlash(filepath.Join(backupPath, mc)))
	}
	args = append(args, filepath.ToSlash(filepath.Join(backupPath, initramfs)))
	return strings.Join(args, " ")
}

type mountInfoEntry struct {
	mountPoint string
}

func grubVisiblePath(hostPath string) string {
	clean := filepath.Clean(hostPath)
	if mountPoint, err := findMountPoint(clean); err == nil && mountPoint != "" {
		if rel, relErr := filepath.Rel(mountPoint, clean); relErr == nil {
			if rel == "." {
				return "/"
			}
			return "/" + filepath.ToSlash(rel)
		}
	}

	for _, mountRoot := range []string{filepath.Clean(EfiDir), filepath.Clean(BootDir)} {
		if clean == mountRoot {
			return "/"
		}
		prefix := mountRoot + string(os.PathSeparator)
		if strings.HasPrefix(clean, prefix) {
			return "/" + filepath.ToSlash(strings.TrimPrefix(clean, prefix))
		}
	}

	return filepath.ToSlash(clean)
}

func findMountPoint(path string) (string, error) {
	entries, err := readMountInfo()
	if err != nil {
		return "", err
	}

	path = filepath.Clean(path)
	best := ""
	for _, entry := range entries {
		mountPoint := filepath.Clean(entry.mountPoint)
		if path != mountPoint && !strings.HasPrefix(path, mountPoint+string(os.PathSeparator)) {
			continue
		}
		if len(mountPoint) > len(best) {
			best = mountPoint
		}
	}
	if best == "" {
		return "", fmt.Errorf("no mount point found for %s", path)
	}
	return best, nil
}

func readMountInfo() ([]mountInfoEntry, error) {
	data, err := os.ReadFile(mountInfoPath)
	if err != nil {
		return nil, err
	}

	var entries []mountInfoEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		entry, ok := parseMountInfoLine(scanner.Text())
		if ok {
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func parseMountInfoLine(line string) (mountInfoEntry, bool) {
	parts := strings.Split(line, " - ")
	if len(parts) != 2 {
		return mountInfoEntry{}, false
	}

	fields := strings.Fields(parts[0])
	if len(fields) < 5 {
		return mountInfoEntry{}, false
	}

	return mountInfoEntry{
		mountPoint: decodeMountInfoPath(fields[4]),
	}, true
}

func decodeMountInfoPath(s string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	out := replacer.Replace(s)
	if !strings.Contains(out, `\`) {
		return out
	}

	var b strings.Builder
	for i := 0; i < len(out); i++ {
		if out[i] == '\\' && i+3 < len(out) {
			if v, err := strconv.ParseInt(out[i+1:i+4], 8, 32); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(out[i])
	}
	return b.String()
}

// AddGrubEntry adds one GRUB menuentry for the EFI copy of a fully synced backup.
func AddGrubEntry(b BootBackup) error {
	name := b.Name
	if name == "" {
		name = filepath.Base(filepath.Clean(b.Path))
	}
	if name == "" || name == "." {
		return fmt.Errorf("invalid backup name")
	}

	canonical := buildBackupFromName(name)
	refreshBackupCompleteness(&canonical)
	if !canonical.HasSnapshot || !canonical.HasEFI || !canonical.InSync {
		return fmt.Errorf("backup %q is not activated in EFI (press 'g' in TUI to activate)", name)
	}
	if !canonical.HasKernel || !canonical.HasInitramfs {
		return fmt.Errorf("backup %q is incomplete", canonical.Path)
	}

	displayPath := canonical.EFIPath
	grubPath := grubVisiblePath(canonical.EFIPath)
	id := backupIDForName(name)
	exists, err := grubEntryExistsByID(id)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	if err := ensureGrubFile(); err != nil {
		return err
	}
	f, err := os.OpenFile(GrubCustom, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	cmdline := currentKernelCmdline()
	if !strings.Contains(cmdline, kernelCmdlineMarker) {
		cmdline = strings.TrimSpace(cmdline + " " + kernelCmdlineMarker + id)
	}
	entry := fmt.Sprintf("cat <<'EOF'\nmenuentry 'Bootrecov %s' --id %s {\n"+
		"    search --file --set=root %s/%s\n"+
		"    linux %s/%s %s\n"+
		"    initrd %s\n"+
		"}\nEOF\n",
		displayPath, id,
		grubPath, canonical.KernelImage,
		grubPath, canonical.KernelImage, cmdline,
		grubInitrdArgs(grubPath, canonical.MicrocodeImages, canonical.InitramfsImage))

	if _, err = f.WriteString(entry); err != nil {
		return err
	}
	return updateGrubConfig()
}

func grubEntryExistsByID(id string) (bool, error) {
	entries, err := ListGrubEntries()
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func currentKernelCmdline() string {
	data, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return "rw"
	}
	fields := strings.Fields(string(data))
	filtered := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.HasPrefix(f, "BOOT_IMAGE=") || strings.HasPrefix(f, "initrd=") {
			continue
		}
		filtered = append(filtered, f)
	}
	if len(filtered) == 0 {
		return "rw"
	}
	return strings.Join(filtered, " ")
}

func ensureGrubFile() error {
	data, err := os.ReadFile(GrubCustom)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(GrubCustom, []byte(grubHeader), 0o755)
		}
		return err
	}
	if !bytes.HasPrefix(data, []byte(grubHeader)) {
		st, statErr := os.Stat(GrubCustom)
		if statErr != nil {
			return statErr
		}
		perm := st.Mode().Perm() | 0o111
		return os.WriteFile(GrubCustom, append([]byte(grubHeader), data...), perm)
	}
	return nil
}

// RemoveGrubEntry removes the entry block matching the GRUB id.
func RemoveGrubEntry(id string) error {
	data, err := os.ReadFile(GrubCustom)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	st, err := os.Stat(GrubCustom)
	if err != nil {
		return err
	}
	perm := st.Mode().Perm() | 0o111

	lines := strings.Split(string(data), "\n")
	var out []string
	skip := false
	for _, l := range lines {
		if skip {
			if strings.TrimSpace(l) == "EOF" {
				skip = false
			}
			continue
		}
		if strings.Contains(l, id) {
			if len(out) > 0 && strings.HasPrefix(strings.TrimSpace(out[len(out)-1]), "cat <<'EOF'") {
				out = out[:len(out)-1]
			}
			skip = true
			continue
		}
		out = append(out, l)
	}
	if err := os.WriteFile(GrubCustom, []byte(strings.Join(out, "\n")), perm); err != nil {
		return err
	}
	return updateGrubConfig()
}

// ListGrubEntries parses GrubCustom and returns bootrecov entries.
func ListGrubEntries() ([]GrubEntry, error) {
	data, err := os.ReadFile(GrubCustom)
	if err != nil {
		if os.IsNotExist(err) {
			return []GrubEntry{}, nil
		}
		return nil, err
	}
	var entries []GrubEntry
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if entry, ok := parseBootrecovMenuentry(line); ok {
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func parseBootrecovMenuentry(line string) (GrubEntry, bool) {
	const prefix = "menuentry 'Bootrecov "
	const middle = "' --id "
	if !strings.HasPrefix(line, prefix) {
		return GrubEntry{}, false
	}
	rest := strings.TrimPrefix(line, prefix)
	titleEnd := strings.Index(rest, middle)
	if titleEnd == -1 {
		return GrubEntry{}, false
	}
	backupPath := rest[:titleEnd]
	idPart := rest[titleEnd+len(middle):]
	idEnd := strings.Index(idPart, " {")
	if idEnd == -1 {
		return GrubEntry{}, false
	}
	id := strings.TrimSpace(idPart[:idEnd])
	if id == "" || !strings.HasPrefix(id, "bootrecov-") {
		return GrubEntry{}, false
	}
	return GrubEntry{
		ID:         id,
		BackupPath: backupPath,
		Name:       filepath.Base(backupPath),
	}, true
}

func removeStaleGrubEntries(backups []BootBackup, preserveByName map[string]struct{}) error {
	valid := map[string]string{}
	for _, b := range backups {
		if _, keep := preserveByName[b.Name]; keep {
			valid[backupIDForName(b.Name)] = b.EFIPath
			continue
		}
		if b.HasSnapshot && b.HasEFI && b.InSync && b.HasKernel && b.HasInitramfs {
			valid[backupIDForName(b.Name)] = b.EFIPath
		}
	}
	entries, err := ListGrubEntries()
	if err != nil {
		return err
	}
	for _, e := range entries {
		expectedPath, ok := valid[e.ID]
		if !ok || e.BackupPath != expectedPath {
			if err := RemoveGrubEntry(e.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func markGrubFlags(backups []BootBackup, entries []GrubEntry) {
	ids := map[string]struct{}{}
	for _, e := range entries {
		ids[e.ID] = struct{}{}
	}
	for i := range backups {
		_, ok := ids[backupIDForName(backups[i].Name)]
		backups[i].GrubEntryExists = ok
	}
}

func backupIDForName(name string) string {
	return backupID(filepath.Join(EfiDir, strings.TrimSpace(name)))
}

func RecoveryCommands(name string) (string, error) {
	canonical := buildBackupFromName(strings.TrimSpace(name))
	refreshBackupCompleteness(&canonical)
	if !canonical.HasSnapshot {
		return "", fmt.Errorf("snapshot %q does not exist", name)
	}
	if !canonical.HasEFI {
		return "", fmt.Errorf("snapshot %q is not activated in EFI (press 'g' in TUI to activate)", name)
	}
	if !canonical.HasKernel || !canonical.HasInitramfs {
		return "", fmt.Errorf("snapshot %q is incomplete", name)
	}

	grubPath := grubVisiblePath(canonical.EFIPath)
	id := backupIDForName(canonical.Name)
	cmdline := currentKernelCmdline()
	if !strings.Contains(cmdline, kernelCmdlineMarker) {
		cmdline = strings.TrimSpace(cmdline + " " + kernelCmdlineMarker + id)
	}
	return strings.Join([]string{
		fmt.Sprintf("search --file --set=root %s/%s", grubPath, canonical.KernelImage),
		fmt.Sprintf("linux %s/%s %s", grubPath, canonical.KernelImage, cmdline),
		fmt.Sprintf("initrd %s", grubInitrdArgs(grubPath, canonical.MicrocodeImages, canonical.InitramfsImage)),
		"boot",
	}, "\n"), nil
}

// ActivateBackup copies a snapshot to EFI (after free-space check) and ensures
// a matching GRUB entry exists.
func ActivateBackup(name string) error {
	canonical := buildBackupFromName(strings.TrimSpace(name))
	refreshBackupCompleteness(&canonical)
	if !canonical.HasSnapshot {
		return fmt.Errorf("snapshot %q does not exist", name)
	}
	if !canonical.HasKernel || !canonical.HasInitramfs {
		return fmt.Errorf("snapshot %q is incomplete", name)
	}
	if !canonical.HasEFI {
		if err := checkEFISpaceForSnapshot(canonical.SnapshotPath); err != nil {
			return err
		}
		if err := ensureEFIMirrorFromSnapshot(&canonical); err != nil {
			return err
		}
	}
	return AddGrubEntry(canonical)
}

// DeactivateBackup removes the GRUB entry and optional EFI mirror, while
// keeping the snapshot in SnapshotDir.
func DeactivateBackup(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("invalid backup name")
	}
	if err := RemoveGrubEntry(backupIDForName(name)); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(EfiDir, name))
}

func updateGrubConfig() error {
	if !AutoUpdateGrub {
		return nil
	}
	if strings.TrimSpace(GrubMkconfig) == "" {
		return fmt.Errorf("grub config regeneration is enabled but grub-mkconfig is not configured")
	}
	if strings.TrimSpace(GrubCfgOutput) == "" {
		return fmt.Errorf("grub config regeneration is enabled but grub.cfg output path is not configured")
	}
	if _, err := exec.LookPath(GrubMkconfig); err != nil {
		return fmt.Errorf("grub config regeneration failed: %s not found in PATH", GrubMkconfig)
	}
	cmd := exec.Command(GrubMkconfig, "-o", GrubCfgOutput)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("grub config regeneration failed: %w: %s", err, strings.TrimSpace(string(out)))
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
		return fmt.Errorf("source directory does not exist: %s", src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	if RcloneBin == "" {
		if RequireRclone {
			return fmt.Errorf("rclone is required but not configured")
		}
		return fallbackSyncCopy(src, dst, normalizeFallbackExcludes(src, excludes))
	}
	if _, err := exec.LookPath(RcloneBin); err != nil {
		if RequireRclone {
			return fmt.Errorf("rclone is required for backup sync but was not found in PATH")
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rclone sync failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func buildRcloneSyncArgs(srcArg, dstArg string, excludes, includes []string, supported map[string]bool) []string {
	args := []string{"sync", srcArg, dstArg}
	for _, flag := range []string{"--links", "--times", "--delete-during", "--perms"} {
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
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, flag+" ") || trimmed == flag {
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
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(mode)
}

func copySymlink(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	link, err := os.Readlink(src)
	if err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.Symlink(link, dst)
}
