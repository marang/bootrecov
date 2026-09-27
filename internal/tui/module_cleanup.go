package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// The boot-time restore hooks write the same two-line marker. The inode binds
// ownership to the restored directory, including across a staging rename.
const restoredModuleMarker = ".bootrecov-restored"

// Tests can override this; production resolves pacman's configured DBPath.
var PacmanDBPath string

var (
	moduleCleanupCommand = func(name string, args ...string) ([]byte, error) {
		cmd := exec.Command(name, args...)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		return cmd.Output()
	}
	moduleCleanupLookPath        = exec.LookPath
	moduleCleanupPackageBusy     = restoredModulePackageBusy
	moduleCleanupHoldPackageLock = holdCleanupPackageLock
)

func markRestoredModuleTree(path, kernelVersion string) error {
	if !validModuleVersion(kernelVersion) {
		return fmt.Errorf("invalid restored kernel version %q", kernelVersion)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("restored module tree is not a directory: %s", path)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot identify restored module tree: %s", path)
	}
	marker := filepath.Join(path, restoredModuleMarker)
	// An archive may contain a marker (even a symlink). Never follow it.
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintf(f, "%s\n%d\n", kernelVersion, st.Ino)
	return errors.Join(writeErr, f.Close())
}

func validModuleVersion(version string) bool {
	return version != "unknown" && validateBackupName(strings.ReplaceAll(version, "+", "-")) == nil && strings.TrimSpace(version) == version
}

func ownedRestoredModuleTree(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}
	marker := filepath.Join(path, restoredModuleMarker)
	mi, err := os.Lstat(marker)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !mi.Mode().IsRegular() || mi.Size() > 512 {
		return false, nil
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) != 3 || lines[2] != "" || lines[0] != filepath.Base(path) || !validModuleVersion(lines[0]) {
		return false, nil
	}
	inode, err := strconv.ParseUint(lines[1], 10, 64)
	if err != nil {
		return false, nil
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && inode == st.Ino, nil
}

// cleanupRestoredModuleTrees only removes trees this installation restored.
// All global protection checks finish before any DKMS or filesystem mutation.
func cleanupRestoredModuleTrees() (result error) {
	root, err := canonicalModuleRoot()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var candidates []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		owned, err := ownedRestoredModuleTree(path)
		if err != nil {
			return fmt.Errorf("inspect restored modules: %w", err)
		}
		if owned {
			candidates = append(candidates, path)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	platform := currentPlatformID()
	if platform != PlatformArch {
		commandOutputSink.emit("Keeping restored modules: package-safe cleanup is unsupported on " + platform)
		return nil
	}
	busy, err := moduleCleanupPackageBusy(platform)
	if err != nil {
		return fmt.Errorf("retain restored modules: check package transaction: %w", err)
	}
	if busy {
		commandOutputSink.emit("Keeping restored modules while a package transaction is active")
		return nil
	}
	release, busy, err := moduleCleanupHoldPackageLock(platform)
	if err != nil {
		return fmt.Errorf("retain restored modules: acquire package transaction lock: %w", err)
	}
	if busy {
		commandOutputSink.emit("Keeping restored modules while a package transaction is active")
		return nil
	}
	defer func() { result = errors.Join(result, release()) }()
	running, err := moduleCleanupCommand("uname", "-r")
	if err != nil || !validModuleVersion(strings.TrimSpace(string(running))) {
		return fmt.Errorf("retain restored modules: cannot determine running kernel: %v", err)
	}
	protected, err := restoredModuleProtectedVersions()
	if err != nil {
		return fmt.Errorf("retain restored modules: %w", err)
	}
	protected[strings.TrimSpace(string(running))] = true
	ownedPaths, err := restoredModulePackagePaths(platform)
	if err != nil {
		return fmt.Errorf("retain restored modules: %w", err)
	}
	var errs []error
	for _, path := range candidates {
		version := filepath.Base(path)
		if protected[version] || modulePathOwned(path, ownedPaths) {
			commandOutputSink.emit("Keeping restored modules for protected kernel " + version)
			continue
		}
		if err := removeRestoredModuleTree(path); err != nil {
			errs = append(errs, fmt.Errorf("retain restored modules %s: %w", version, err))
		}
	}
	return errors.Join(errs...)
}

func removeRestoredModuleTree(path string) error {
	if err := restoredModuleMountCheck(path); err != nil {
		return err
	}
	owned, err := ownedRestoredModuleTree(path)
	if err != nil || !owned {
		return err
	}
	if err := removeRestoredDKMS(filepath.Base(path)); err != nil {
		return err
	}
	// The package lock remains held while DKMS runs. Recheck the marker and
	// mounts before removal because unrelated privileged processes may act.
	if err := restoredModuleMountCheck(path); err != nil {
		return err
	}
	owned, err = ownedRestoredModuleTree(path)
	if err != nil || !owned {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	commandOutputSink.emit("Removed unused restored modules for kernel " + filepath.Base(path))
	return nil
}

func holdCleanupPackageLock(platform string) (func() error, bool, error) {
	if platform != PlatformArch {
		return nil, false, fmt.Errorf("unsupported package lock on %s", platform)
	}
	dbPath, err := effectivePacmanDBPath()
	if err != nil {
		return nil, false, err
	}
	path := filepath.Join(dbPath, "db.lck")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, false, err
	}
	return func() error { return errors.Join(file.Close(), os.Remove(path)) }, false, nil
}

func effectivePacmanDBPath() (string, error) {
	configured := strings.TrimSpace(PacmanDBPath)
	if configured == "" {
		out, err := moduleCleanupCommand("pacman-conf", "DBPath")
		if err != nil {
			return "", fmt.Errorf("query pacman DBPath: %w", err)
		}
		configured = strings.TrimSpace(string(out))
	}
	if !filepath.IsAbs(configured) || strings.ContainsAny(configured, "\n\x00") {
		return "", fmt.Errorf("invalid pacman DBPath %q", configured)
	}
	return filepath.EvalSymlinks(filepath.Clean(configured))
}

func restoredModuleMountCheck(path string) error {
	data, err := os.ReadFile(mountInfoPath)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		entry, ok := parseMountInfoLine(line)
		if !ok || !filepath.IsAbs(entry.mountPoint) {
			return fmt.Errorf("cannot parse mount table")
		}
		mount := filepath.Clean(entry.mountPoint)
		if mount == path || strings.HasPrefix(mount, path+string(os.PathSeparator)) {
			return fmt.Errorf("module tree contains mountpoint %s", mount)
		}
	}
	return nil
}

func restoredModulePackageBusy(platform string) (bool, error) {
	if platform != PlatformArch {
		return false, fmt.Errorf("package ownership cleanup is unsupported on %s", platform)
	}
	dbPath, err := effectivePacmanDBPath()
	if err != nil {
		return false, err
	}
	_, err = os.Lstat(filepath.Join(dbPath, "db.lck"))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func restoredModulePackagePaths(platform string) ([]string, error) {
	if platform != PlatformArch {
		return nil, fmt.Errorf("package ownership cleanup is unsupported on %s", platform)
	}
	data, err := moduleCleanupCommand("pacman", "-Qlq")
	if err != nil {
		return nil, fmt.Errorf("query installed package files: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, fmt.Errorf("installed package file list is empty")
	}
	root, err := canonicalModuleRoot()
	if err != nil {
		return nil, fmt.Errorf("resolve modules directory: %w", err)
	}
	// Package databases use both /lib/modules and /usr/lib/modules on
	// merged-/usr installations. Resolve these roots once, then compare
	// lexical prefixes; do not stat hundreds of thousands of package files.
	aliases := []string{filepath.Clean(RootModulesDir), root}
	for _, alias := range []string{"/lib/modules", "/usr/lib/modules"} {
		resolved, err := filepath.EvalSymlinks(alias)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("resolve module alias %s: %w", alias, err)
		}
		if resolved == root {
			aliases = append(aliases, alias)
		}
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if !filepath.IsAbs(line) || strings.ContainsRune(line, '\x00') {
			return nil, fmt.Errorf("cannot parse installed package file %q", line)
		}
		path := filepath.Clean(line)
		for _, alias := range aliases {
			if path == alias || strings.HasPrefix(path, alias+string(os.PathSeparator)) {
				paths = append(paths, root+strings.TrimPrefix(path, alias))
				break
			}
		}
	}
	return paths, nil
}

func canonicalModuleRoot() (string, error) {
	root, err := filepath.EvalSymlinks(RootModulesDir)
	if err != nil {
		return "", err
	}
	return filepath.Abs(root)
}

func modulePathOwned(path string, files []string) bool {
	for _, file := range files {
		if file == path || strings.HasPrefix(file, path+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func restoredModuleProtectedVersions() (map[string]bool, error) {
	protected := map[string]bool{}
	entries, err := ListGrubEntries()
	if err != nil {
		return nil, err
	}
	// ListGrubEntries intentionally tolerates malformed records for display;
	// cleanup must not silently overlook a potentially active entry.
	if data, err := os.ReadFile(GrubCustom); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "menuentry") && strings.Contains(line, "Bootrecov") {
				if _, ok := parseBootrecovMenuentry(line); !ok {
					return nil, fmt.Errorf("unrecognized Bootrecov entry")
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	bls, err := os.ReadDir(BLSEntriesDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range bls {
		if strings.HasPrefix(entry.Name(), "bootrecov-") && strings.HasSuffix(entry.Name(), ".conf") {
			if _, ok := parseBLSEntryFile(strings.TrimSuffix(entry.Name(), ".conf"), filepath.Join(BLSEntriesDir, entry.Name())); !ok {
				return nil, fmt.Errorf("unrecognized Bootrecov BLS entry %s", entry.Name())
			}
		}
	}
	for _, entry := range entries {
		b := buildBackupFromName(entry.Name)
		refreshBackupCompleteness(&b)
		if !validModuleVersion(b.KernelVersion) {
			return nil, fmt.Errorf("active recovery %s has unknown kernel version", entry.Name)
		}
		protected[b.KernelVersion] = true
	}
	backups, err := DiscoverBackups()
	if err != nil {
		return nil, err
	}
	for _, b := range backups {
		if b.HasSnapshot && !b.HasArchivedModules {
			if !validModuleVersion(b.KernelVersion) {
				return nil, fmt.Errorf("snapshot %s has unknown kernel version and no module archive", b.Name)
			}
			protected[b.KernelVersion] = true
		}
	}
	if err := protectPrimaryModuleVersions(protected); err != nil {
		return nil, err
	}
	return protected, nil
}

// Scan every primary kernel image, rather than only the first/default image.
// Also inspect configured GRUB/BLS references for kernels outside BootDir.
func protectPrimaryModuleVersions(protected map[string]bool) error {
	bootRoot, err := filepath.EvalSymlinks(BootDir)
	if err != nil {
		return fmt.Errorf("resolve primary boot directory: %w", err)
	}
	efiRoot, err := filepath.EvalSymlinks(EfiDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("resolve recovery mirror: %w", err)
	}
	snapshotRoot, err := filepath.EvalSymlinks(SnapshotDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("resolve snapshot directory: %w", err)
	}
	err = filepath.WalkDir(bootRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != bootRoot && (path == efiRoot || path == snapshotRoot || entry.Name() == "bootrecov-snapshots") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".efi") && strings.Contains(filepath.ToSlash(path), "/EFI/Linux/") {
			return fmt.Errorf("cannot identify kernel version of unified kernel image %s", path)
		}
		if strings.HasPrefix(entry.Name(), "vmlinuz") || strings.HasPrefix(entry.Name(), "vmlinux") {
			return protectModuleKernelImage(path, protected)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("inspect primary boot kernels: %w", err)
	}
	configs := []string{GrubCfgOutput}
	entries, err := os.ReadDir(BLSEntriesDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".conf") && !strings.HasPrefix(entry.Name(), "bootrecov-") {
			configs = append(configs, filepath.Join(BLSEntriesDir, entry.Name()))
		}
	}
	seenConfigs := map[string]bool{}
	probeCache := map[string]string{}
	grubPrefix := filepath.Dir(GrubCfgOutput)
	for _, config := range configs {
		deviceState := &cleanupGRUBDevices{probes: probeCache}
		if err := protectBootConfigKernels(config, filepath.Dir(config), grubPrefix, protected, seenConfigs, false, deviceState); err != nil {
			return err
		}
	}
	return nil
}

func protectBootConfigKernels(config, configDir, grubPrefix string, protected map[string]bool, seen map[string]bool, referenced bool, devices *cleanupGRUBDevices) error {
	canonical, err := filepath.EvalSymlinks(config)
	if os.IsNotExist(err) && !referenced {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve boot config %s: %w", config, err)
	}
	// The same source file can be executed under different config_directory
	// values. Only configfile changes that directory; source preserves it.
	key := canonical + "\x00" + configDir + devices.key()
	if seen[key] {
		return nil
	}
	seen[key] = true
	data, err := os.ReadFile(canonical)
	if err != nil {
		return fmt.Errorf("read boot config %s: %w", canonical, err)
	}
	if strings.Contains(string(data), "\\\n") {
		return fmt.Errorf("unsupported GRUB line continuation in %s", canonical)
	}
	lines, err := cleanupKnownMenuentries(strings.Split(string(data), "\n"), protected)
	if err != nil {
		return err
	}
	for _, line := range lines {
		fields := strings.Fields(strings.ReplaceAll(line, ";", " ; "))
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		// GRUB scripts can reassign these variables at runtime. The paths
		// passed into this scan then no longer describe what GRUB will read.
		for _, field := range fields {
			field = strings.Trim(field, "{}\"'")
			if strings.HasPrefix(field, "prefix=") || strings.HasPrefix(field, "config_directory=") {
				return fmt.Errorf("boot config %s changes GRUB source directory", canonical)
			}
		}
		if fields[0] == "unset" && len(fields) > 1 && (strings.Trim(fields[1], "\"'") == "prefix" || strings.Trim(fields[1], "\"'") == "config_directory") {
			return fmt.Errorf("boot config %s unsets GRUB source directory", canonical)
		}
		menuentryHeader := fields[0] == "menuentry"
		// The literal [ command consumes test operands. Stop at ] or a
		// command separator, so a following executable EFI command is
		// never mistaken for a comparison value.
		testArguments := len(fields) > 1 && (fields[0] == "if" || fields[0] == "elif") && fields[1] == "["
		for i, field := range fields {
			if field == "{" {
				menuentryHeader = false
			}
			if strings.ContainsAny(field, "];&|") || field == "{" || field == "}" {
				testArguments = false
			}
			command := strings.Trim(field, "{}\"'")
			commandStart := i == 0 || fields[i-1] == ";" || fields[i-1] == "{" || fields[i-1] == "then" || fields[i-1] == "else" || strings.HasPrefix(field, "{")
			// Bare NAME=value is GRUB assignment syntax, not a command
			// whose name is expanded. Inspect the literal name separately;
			// root and source-directory mutations remain guarded below/above.
			assignmentName, _, assignment := strings.Cut(command, "=")
			literalAssignment := assignment && assignmentName != "" && strings.IndexFunc(assignmentName, func(r rune) bool {
				return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_')
			}) == -1 && !(assignmentName[0] >= '0' && assignmentName[0] <= '9')
			if commandStart && !literalAssignment && strings.ContainsAny(command, "\"'\\$") {
				return fmt.Errorf("unsupported GRUB command syntax in %s", canonical)
			}
			if command == "set" && i+1 < len(fields) {
				name, _, _ := strings.Cut(strings.Trim(fields[i+1], "\"'"), "=")
				if strings.ContainsAny(name, "\"'\\$") {
					return fmt.Errorf("unsupported GRUB assignment syntax in %s", canonical)
				}
			}
			if command == "unset" && i+1 < len(fields) && strings.Trim(fields[i+1], "\"'") == "root" {
				return fmt.Errorf("boot config %s unsets GRUB root", canonical)
			}
			if command == "set" && i+1 < len(fields) && strings.HasPrefix(strings.Trim(fields[i+1], "\"'"), "root=") {
				if err := devices.selectRoot("drive", strings.Trim(strings.TrimPrefix(strings.Trim(fields[i+1], "\"'"), "root="), "\"'")); err != nil {
					return err
				}
			}
			if strings.HasPrefix(command, "root=") && (i == 0 || fields[i-1] == ";" || fields[i-1] == "{" || fields[i-1] == "then" || fields[i-1] == "else") {
				return fmt.Errorf("unrecognized GRUB root assignment in %s", canonical)
			}
			if command == "search" || strings.HasPrefix(command, "search.") {
				end := i + 1
				for end < len(fields) && fields[end] != ";" && fields[end] != "}" {
					end++
				}
				searchFields := append([]string{command}, fields[i+1:end]...)
				uuid, err := cleanupGRUBSearchUUID(searchFields)
				if err != nil {
					return err
				}
				if err := devices.selectRoot("fs_uuid", uuid); err != nil {
					return err
				}
			}
			// --class efi labels a menuentry; it is not an EFI command.
			// End this exception at the opening brace so inline commands
			// and condition commands still take the conservative path.
			if command == "efi" && (testArguments || menuentryHeader && i > 0 && fields[i-1] == "--class") {
				continue
			}
			if command == "bootnext" {
				return fmt.Errorf("cannot identify kernel version of GRUB BootNext target in %s", canonical)
			}
			if command == "efi" || command == "chainloader" {
				return fmt.Errorf("cannot identify kernel version of EFI boot reference in %s", canonical)
			}
			if command == "linux" || command == "linuxefi" || command == "linux16" {
				if i+1 >= len(fields) {
					return fmt.Errorf("unrecognized boot kernel reference in %s", canonical)
				}
				ref := strings.Trim(fields[i+1], "\"'{}")
				if len(devices.roots) == 0 {
					if err := protectBootKernelReference(ref, protected); err != nil {
						return err
					}
				} else {
					paths, err := devices.paths(ref)
					if err != nil {
						return err
					}
					for _, path := range paths {
						if _, err := os.Stat(path); err != nil {
							return fmt.Errorf("resolve selected GRUB kernel %s: %w", path, err)
						}
						if err := protectModuleKernelImage(path, protected); err != nil {
							return err
						}
					}
				}
			}
			if command != "source" && command != "configfile" {
				continue
			}
			if i+1 >= len(fields) {
				return fmt.Errorf("missing %s target in %s", command, canonical)
			}
			ref := strings.Trim(fields[i+1], "\"';")
			candidates, optional, err := bootConfigSourcePaths(ref, configDir, grubPrefix)
			if err != nil {
				return err
			}
			// prefix/config_directory carry their own device. Absolute paths,
			// including ($root), instead refer to the currently selected root.
			if len(devices.roots) > 0 && !strings.HasPrefix(ref, "$config_directory/") && !strings.HasPrefix(ref, "${config_directory}/") && !strings.HasPrefix(ref, "$prefix/") && !strings.HasPrefix(ref, "${prefix}/") {
				candidates, err = devices.paths(ref)
				if err != nil {
					return err
				}
			}
			optional = optional && canonical == GrubCfgOutput
			found := false
			for _, path := range uniqueStrings(candidates) {
				if _, err := os.Stat(path); os.IsNotExist(err) {
					if len(devices.roots) > 0 && !optional {
						return fmt.Errorf("cannot resolve selected GRUB source %s", path)
					}
					continue
				} else if err != nil {
					return err
				}
				found = true
				nextConfigDir := configDir
				if command == "configfile" {
					nextConfigDir = filepath.Dir(path)
				}
				if err := protectBootConfigKernels(path, nextConfigDir, grubPrefix, protected, seen, true, devices); err != nil {
					return err
				}
			}
			if !found && !optional {
				return fmt.Errorf("cannot resolve %s target %s in %s", command, ref, canonical)
			}
		}
	}
	return nil
}

// Keep every proved root seen in conditional/menuentry bodies. This scanner
// deliberately does not evaluate GRUB control flow; uncertain alternatives must
// retain modules rather than let a later assignment hide an earlier boot path.
type cleanupGRUBDevices struct {
	roots  []mountInfoEntry
	probes map[string]string
	mounts []mountInfoEntry
}

func (d *cleanupGRUBDevices) key() string {
	var key string
	for _, root := range d.roots {
		key += "\x00" + root.mountPoint + ":" + root.mountRoot
	}
	return key
}

func (d *cleanupGRUBDevices) selectRoot(target, value string) error {
	value = strings.Trim(value, "()")
	if value == "" || strings.ContainsAny(value, "$\"'{}; \t\n") {
		return fmt.Errorf("unresolved GRUB root %q", value)
	}
	data, err := os.ReadFile(mountInfoPath)
	if err != nil {
		return err
	}
	var entries []mountInfoEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		entry, ok := parseMountInfoLine(line)
		if !ok || !filepath.IsAbs(entry.mountPoint) || !filepath.IsAbs(entry.mountRoot) {
			return fmt.Errorf("cannot parse GRUB mount table")
		}
		entries = append(entries, entry)
	}
	d.mounts = entries
	matched := false
	for _, entry := range entries {
		// Only disk filesystems GRUB can plausibly read. Other filesystem
		// types and unmounted devices remain unsupported and fail closed.
		switch entry.fsType {
		case "ext2", "ext3", "ext4", "btrfs", "xfs", "vfat", "f2fs":
		default:
			continue
		}
		probeKey := target + "\x00" + entry.mountPoint
		probe, ok := d.probes[probeKey]
		if !ok {
			out, err := moduleCleanupCommand("grub-probe", "--target="+target, "--", entry.mountPoint)
			if err == nil {
				probe = strings.Trim(strings.TrimSpace(string(out)), "()")
			}
			d.probes[probeKey] = probe
		}
		if probe != value {
			continue
		}
		matched = true
		present := false
		for _, root := range d.roots {
			if root == entry {
				present = true
			}
		}
		if !present {
			d.roots = append(d.roots, entry)
		}
	}
	if !matched {
		return fmt.Errorf("cannot prove mounted GRUB root %s=%s", target, value)
	}
	return nil
}

func (d *cleanupGRUBDevices) paths(ref string) ([]string, error) {
	ref = strings.TrimPrefix(ref, "($root)")
	if !filepath.IsAbs(ref) || strings.ContainsAny(ref, "$()") {
		return nil, fmt.Errorf("unresolved selected GRUB path %s", ref)
	}
	ref = filepath.Clean(ref)
	var paths []string
	for _, root := range d.roots {
		mountRoot := filepath.Clean(root.mountRoot)
		if mountRoot != "/" && ref != mountRoot && !strings.HasPrefix(ref, mountRoot+"/") {
			return nil, fmt.Errorf("GRUB path %s is outside mounted filesystem root %s", ref, mountRoot)
		}
		path := filepath.Join(root.mountPoint, strings.TrimPrefix(strings.TrimPrefix(ref, mountRoot), "/"))
		// A nested mount or symlink may expose a different device or subtree.
		// Require the actual image/source to retain the proved mapping.
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, fmt.Errorf("resolve selected GRUB path %s: %w", path, err)
		}
		var actual mountInfoEntry
		for _, mount := range d.mounts {
			prefix := strings.TrimSuffix(filepath.Clean(mount.mountPoint), "/") + "/"
			if resolved == mount.mountPoint || strings.HasPrefix(resolved, prefix) {
				if len(mount.mountPoint) > len(actual.mountPoint) {
					actual = mount
				}
			}
		}
		if actual != root {
			return nil, fmt.Errorf("GRUB path %s crosses its proved mount", path)
		}
		paths = append(paths, resolved)
	}
	return uniqueStrings(paths), nil
}

func cleanupGRUBSearchUUID(fields []string) (string, error) {
	// Recognize the UUID searches emitted by grub-mkconfig, including its
	// platform hint branch. Other search modes/variables are not evaluated.
	if len(fields) < 2 || fields[0] != "search" {
		return "", fmt.Errorf("unrecognized GRUB search")
	}
	fsUUID, setRoot := false, false
	var uuid string
	for _, raw := range fields[1:] {
		field := strings.Trim(raw, "\"'")
		switch {
		case field == "--fs-uuid":
			fsUUID = true
		case field == "--set=root" || field == "--set":
			setRoot = true
		case field == "--no-floppy":
		case strings.HasPrefix(field, "--hint-bios=") || strings.HasPrefix(field, "--hint-efi=") || strings.HasPrefix(field, "--hint-baremetal="):
		case !strings.HasPrefix(field, "-") && uuid == "":
			uuid = field
		default:
			return "", fmt.Errorf("unrecognized GRUB search option %s", field)
		}
	}
	if !fsUUID || !setRoot || uuid == "" {
		return "", fmt.Errorf("unresolved GRUB root search")
	}
	return uuid, nil
}

func bootConfigSourcePaths(ref, configDir, grubPrefix string) ([]string, bool, error) {
	for _, variable := range []string{"${config_directory}/", "$config_directory/"} {
		if strings.HasPrefix(ref, variable) {
			relative := strings.TrimPrefix(ref, variable)
			if relative == "" || strings.ContainsAny(relative, "$()") {
				return nil, false, fmt.Errorf("unresolved boot config source %s", ref)
			}
			return []string{filepath.Join(configDir, relative)}, relative == "custom.cfg", nil
		}
	}
	for _, variable := range []string{"${prefix}/", "$prefix/"} {
		if strings.HasPrefix(ref, variable) {
			relative := strings.TrimPrefix(ref, variable)
			if relative == "" || strings.ContainsAny(relative, "$()") {
				return nil, false, fmt.Errorf("unresolved boot config source %s", ref)
			}
			return []string{filepath.Join(grubPrefix, relative)}, relative == "custom.cfg", nil
		}
	}
	if strings.HasPrefix(ref, "($root)") {
		ref = strings.TrimPrefix(ref, "($root)")
	}
	if ref == "" || strings.ContainsAny(ref, "$()") {
		return nil, false, fmt.Errorf("unresolved boot config source %s", ref)
	}
	if filepath.IsAbs(ref) {
		return []string{ref, filepath.Join(BootDir, strings.TrimPrefix(ref, "/"))}, false, nil
	}
	// A bare GRUB path is resolved against its boot device, not reliably
	// against the directory of the host file we happen to be reading.
	return nil, false, fmt.Errorf("unresolved relative boot config source %s", ref)
}

func protectBootKernelReference(ref string, protected map[string]bool) error {
	// Resolve GRUB's common ($root) prefix, but fail closed on other variables.
	if strings.HasPrefix(ref, "($root)") {
		ref = strings.TrimPrefix(ref, "($root)")
	}
	if strings.ContainsAny(ref, "$()") {
		return fmt.Errorf("unresolved boot kernel reference %s", ref)
	}
	mirrorPath, mirrorRef, err := recoveryMirrorKernelPath(ref)
	if err != nil {
		return err
	}
	candidates := []string{ref, filepath.Join(BootDir, strings.TrimPrefix(ref, "/"))}
	if mirrorRef {
		candidates = append([]string{mirrorPath}, candidates...)
	}
	found := false
	for _, path := range uniqueStrings(candidates) {
		if !filepath.IsAbs(path) {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			if err := protectModuleKernelImage(path, protected); err != nil {
				return err
			}
			found = true
			// Different filesystem mappings may resolve to different images.
			// Retain every identified version rather than guessing which boots.
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if !found {
		if mirrorRef {
			return fmt.Errorf("cannot resolve recovery boot kernel %s", ref)
		}
		if version := parseKernelVersionFromName(filepath.Base(ref)); validModuleVersion(version) {
			protected[version] = true
			return nil
		}
		return fmt.Errorf("cannot resolve primary boot kernel %s", ref)
	}
	return nil
}

func recoveryMirrorKernelPath(ref string) (string, bool, error) {
	mirror := filepath.Clean(EfiDir)
	visible := filepath.Clean(grubVisiblePath(mirror))
	for _, prefix := range uniqueStrings([]string{mirror, visible}) {
		if strings.HasPrefix(ref, prefix+"/") {
			return joinedRecoveryKernelPath(mirror, strings.TrimPrefix(ref, prefix+"/"))
		}
	}
	const segment = "/bootrecov-snapshots/"
	if index := strings.Index(ref, segment); index >= 0 {
		if filepath.Base(mirror) != "bootrecov-snapshots" {
			return "", true, fmt.Errorf("cannot map recovery boot kernel %s to mirror %s", ref, mirror)
		}
		return joinedRecoveryKernelPath(mirror, ref[index+len(segment):])
	}
	return "", false, nil
}

func joinedRecoveryKernelPath(mirror, relative string) (string, bool, error) {
	path := filepath.Join(mirror, relative)
	inside, err := filepath.Rel(mirror, path)
	if err != nil || inside == "." || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return "", true, fmt.Errorf("invalid recovery boot kernel path %s", relative)
	}
	return path, true, nil
}

func protectModuleKernelImage(path string, protected map[string]bool) error {
	data, err := moduleCleanupCommand("file", "-b", "-L", "--", path)
	if err != nil {
		return fmt.Errorf("identify primary kernel %s: %w", path, err)
	}
	_, rest, found := strings.Cut(string(data), " version ")
	version := ""
	if found {
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			version = strings.TrimRight(fields[0], ",(")
		}
	}
	if !validModuleVersion(version) {
		return fmt.Errorf("primary kernel %s has unknown version", path)
	}
	protected[version] = true
	// Keep the named version too. A mismatched filename is suspicious, and
	// neither version should be discarded while the boot image exists.
	if named := parseKernelVersionFromName(filepath.Base(path)); validModuleVersion(named) {
		protected[named] = true
	}
	return nil
}

type restoredDKMSTarget struct{ module, version string }

func removeRestoredDKMS(kernel string) error {
	if _, err := moduleCleanupLookPath("dkms"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("locate dkms: %w", err)
	}
	data, err := moduleCleanupCommand("dkms", "status")
	if err != nil {
		return fmt.Errorf("query DKMS: %w", err)
	}
	var targets []restoredDKMSTarget
	seen := map[restoredDKMSTarget]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields, status, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(status) == "" {
			return fmt.Errorf("cannot parse DKMS status %q", line)
		}
		parts := strings.Split(fields, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		module, version, slash := strings.Cut(parts[0], "/")
		// Older DKMS prints module, version, kernel, arch instead.
		if !slash && (len(parts) == 2 || len(parts) == 4) {
			module, version, parts = parts[0], parts[1], append([]string{parts[0] + "/" + parts[1]}, parts[2:]...)
		}
		if !validModuleVersion(module) || !validModuleVersion(version) || (len(parts) != 1 && len(parts) != 3) {
			return fmt.Errorf("cannot parse DKMS status %q", line)
		}
		if len(parts) == 1 {
			continue
		} // Source registered, no kernel build.
		if !validModuleVersion(parts[1]) || !validModuleVersion(parts[2]) {
			return fmt.Errorf("cannot parse DKMS status %q", line)
		}
		if parts[1] != kernel {
			continue
		}
		target := restoredDKMSTarget{module, version}
		if !seen[target] {
			targets = append(targets, target)
			seen[target] = true
		}
	}
	for _, target := range targets {
		if _, err := moduleCleanupCommand("dkms", "remove", "-m", target.module, "-v", target.version, "-k", kernel); err != nil {
			return fmt.Errorf("remove DKMS %s/%s for %s: %w", target.module, target.version, kernel, err)
		}
	}
	return nil
}
