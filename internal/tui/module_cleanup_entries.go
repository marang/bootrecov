package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Recognize whole entries before the general script scan. Their root searches
// belong only to that menuentry: neither Windows setup nor a recovery's file
// search should contaminate the device mapping of another Linux entry.
// Anything outside these narrow shapes still goes through the fail-closed scan.
func cleanupKnownMenuentries(lines []string, protected map[string]bool) ([]string, error) {
	var result []string
	for i := 0; i < len(lines); i++ {
		header := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(header, "menuentry ") || !strings.HasSuffix(header, " {") {
			result = append(result, lines[i])
			continue
		}
		end := i + 1
		for end < len(lines) && strings.TrimSpace(lines[end]) != "}" {
			end++
		}
		if end == len(lines) {
			result = append(result, lines[i])
			continue
		}
		body := lines[i+1 : end]
		// A header is still GRUB code. Never let an inline command or a
		// brace-bearing construct hide behind a recognized body.
		plainHeader := !strings.ContainsAny(strings.TrimSuffix(header, " {"), ";{}\\")
		known := plainHeader && cleanupWindowsEntry(body)
		if !known {
			var err error
			known, err = cleanupGeneratedRecoveryEntry(header, body, protected)
			if err != nil {
				return nil, err
			}
		}
		if known {
			i = end
			continue
		}
		result = append(result, lines[i])
	}
	return result, nil
}

func cleanupWindowsEntry(body []string) bool {
	chainloaders := 0
	for _, line := range body {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if fields[0] != "if" && strings.ContainsAny(line, ";{}\\") {
			return false
		}
		switch fields[0] {
		case "chainloader":
			if len(fields) != 2 {
				return false
			}
			ref := strings.Trim(fields[1], "\"'")
			ref = strings.TrimPrefix(ref, "($root)")
			// No basename, title or class heuristic: shim, GRUB, UKIs and legacy
			// sector chainloading can all lead to Linux and remain unsupported.
			if !strings.EqualFold(ref, "/EFI/Microsoft/Boot/bootmgfw.efi") {
				return false
			}
			chainloaders++
		case "insmod":
			if len(fields) != 2 {
				return false
			}
			switch fields[1] {
			case "part_gpt", "part_msdos", "fat", "ntfs", "chain":
			default:
				return false
			}
		case "set":
			if len(fields) != 2 {
				return false
			}
			assignment := strings.Trim(fields[1], "\"'")
			if !strings.HasPrefix(assignment, "root=") {
				return false
			}
			value := strings.Trim(strings.TrimPrefix(assignment, "root="), "\"'")
			if value == "" || strings.ContainsAny(value, "$;{}\\\"'") {
				return false
			}
		case "search":
			if _, err := cleanupGRUBSearchUUID(fields); err != nil {
				return false
			}
		case "if":
			if line != "if [ x$feature_platform_search_hint = xy ]; then" {
				return false
			}
		case "else", "fi", "boot", "savedefault":
			if len(fields) != 1 {
				return false
			}
		default:
			return false
		}
	}
	return chainloaders == 1
}

func cleanupGeneratedRecoveryEntry(header string, body []string, protected map[string]bool) (bool, error) {
	entry, ok := parseBootrecovMenuentry(header)
	if !ok || validateBackupName(entry.Name) != nil || entry.ID != backupIDForName(entry.Name) || entry.BackupPath != filepath.Join(EfiDir, entry.Name) {
		return false, nil
	}
	if header != fmt.Sprintf("menuentry 'Bootrecov %s' --id %s {", entry.BackupPath, entry.ID) {
		return false, nil
	}
	// Match only the shape written by addGrubEntry, never arbitrary commands
	// inside a menuentry bearing a Bootrecov title.
	if len(body) != 3 {
		return false, nil
	}
	b := buildBackupFromName(entry.Name)
	refreshBackupCompleteness(&b)
	if !b.HasSnapshot || !b.HasEFI || !validModuleVersion(b.KernelVersion) {
		return false, nil
	}
	image := grubVisiblePath(b.EFIPath) + "/" + b.KernelImage
	if strings.TrimSpace(body[0]) != "search --file --set=root "+image {
		return false, nil
	}
	linux := strings.TrimSpace(body[1])
	if !strings.HasPrefix(linux, "linux "+image+" ") || strings.ContainsAny(linux, ";{}\\") {
		return false, nil
	}
	if strings.TrimSpace(body[2]) != "initrd "+grubInitrdArgs(grubVisiblePath(b.EFIPath), b.MicrocodeImages, b.InitramfsImage) {
		return false, nil
	}
	entries, err := ListGrubEntries()
	if err != nil {
		return false, err
	}
	active := false
	for _, current := range entries {
		if current == entry {
			active = true
			break
		}
	}
	if !active {
		return false, nil
	}
	// Inspect the actual active mirror too, rather than relying on its title
	// or snapshot metadata to substitute for the image GRUB will boot.
	if err := protectModuleKernelImage(filepath.Join(b.EFIPath, b.KernelImage), protected); err != nil {
		return false, fmt.Errorf("inspect active recovery %s: %w", b.Name, err)
	}
	if err := protectRecoveryFileSearch(image, protected); err != nil {
		return false, err
	}
	protected[b.KernelVersion] = true
	return true, nil
}

// search --file may select another filesystem containing the same path (for
// example a cloned ESP). Protect all visible matches, including images whose
// contents disagree with the local snapshot or the filename.
func protectRecoveryFileSearch(ref string, protected map[string]bool) error {
	data, err := os.ReadFile(mountInfoPath)
	if err != nil {
		return err
	}
	var mounts []mountInfoEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		mount, ok := parseMountInfoLine(line)
		if !ok || !filepath.IsAbs(mount.mountPoint) || !filepath.IsAbs(mount.mountRoot) {
			return fmt.Errorf("cannot parse recovery file-search mount table")
		}
		mounts = append(mounts, mount)
	}
	found := false
	for _, mount := range mounts {
		// Do not exclude a matching image solely by filesystem type: GRUB
		// can read more types than our UUID/device resolver supports.
		root := filepath.Clean(mount.mountRoot)
		if root != "/" && ref != root && !strings.HasPrefix(ref, root+"/") {
			continue
		}
		path := filepath.Join(mount.mountPoint, strings.TrimPrefix(strings.TrimPrefix(ref, root), "/"))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		devices := cleanupGRUBDevices{roots: []mountInfoEntry{mount}, mounts: mounts}
		paths, err := devices.paths(ref)
		if err != nil {
			return err
		}
		for _, image := range paths {
			if err := protectModuleKernelImage(image, protected); err != nil {
				return err
			}
			found = true
		}
	}
	if !found {
		return fmt.Errorf("cannot prove mounted recovery file-search target %s", ref)
	}
	return nil
}
