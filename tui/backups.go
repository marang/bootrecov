package tui

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BootBackup represents a backup of /boot
// Directory is expected to contain vmlinuz and initramfs images
// Example: /boot/efi/boot-backups/2024-05-01-120000

type BootBackup struct {
	Path            string
	HasKernel       bool
	HasInitramfs    bool
	GrubEntryExists bool
}

var (
	SnapshotDir = "/var/backups/boot-snapshots"
	EfiDir      = "/boot/efi/boot-backups"
	GrubCustom  = "/etc/grub.d/41_custom_boot_backups"
	grubHeader  = "#!/bin/bash\n"
)

// DiscoverBackups scans known backup directories and returns BootBackup entries.
func DiscoverBackups() ([]BootBackup, error) {
	var backups []BootBackup
	dirs := []string{SnapshotDir, EfiDir}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// Skip if directory doesn't exist
			if os.IsNotExist(err) {
				continue
			}
			return backups, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			b := BootBackup{Path: filepath.Join(dir, e.Name())}
			b.HasKernel = fileExists(filepath.Join(b.Path, "vmlinuz-linux")) || fileExists(filepath.Join(b.Path, "vmlinuz"))
			b.HasInitramfs = fileExists(filepath.Join(b.Path, "initramfs-linux.img")) || fileExists(filepath.Join(b.Path, "initrd.img"))
			b.GrubEntryExists = grubEntryExists(e.Name())
			backups = append(backups, b)
		}
	}
	return backups, nil
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

// grubEntryExists checks /etc/grub.d/41_custom_boot_backups for an entry
func grubEntryExists(name string) bool {
	f, err := os.Open(GrubCustom)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	id := fmt.Sprintf("bootrecov-%s", name)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), id) {
			return true
		}
	}
	return false
}

// AddGrubEntry appends a GRUB menuentry for the backup
func AddGrubEntry(b BootBackup) error {
	// ensure header exists before appending
	if err := ensureGrubFile(); err != nil {
		return err
	}
	f, err := os.OpenFile(GrubCustom, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	entry := fmt.Sprintf("cat <<'EOF'\nmenuentry 'Bootrecov %s' --id bootrecov-%s {\n"+
		"    search --file --set=root %s/vmlinuz-linux\n"+
		"    linux %s/vmlinuz-linux root=UUID=your-root rw\n"+
		"    initrd %s/initramfs-linux.img\n"+
		"}\nEOF\n",
		b.Path, filepath.Base(b.Path), b.Path, b.Path, b.Path)

	if _, err := f.WriteString(entry); err != nil {
		return err
	}
	return nil
}

// ensureGrubFile creates GrubCustom with header if missing
func ensureGrubFile() error {
	data, err := os.ReadFile(GrubCustom)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(GrubCustom, []byte(grubHeader), 0755)
		}
		return err
	}
	if !bytes.HasPrefix(data, []byte(grubHeader)) {
		return os.WriteFile(GrubCustom, append([]byte(grubHeader), data...), 0755)
	}
	return nil
}

// RemoveGrubEntry removes entries matching the backup name
func RemoveGrubEntry(name string) error {
	data, err := os.ReadFile(GrubCustom)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	var out []string
	id := fmt.Sprintf("bootrecov-%s", name)
	skip := false
	for _, l := range lines {
		if skip {
			if strings.TrimSpace(l) == "EOF" {
				skip = false
			}
			continue
		}
		if strings.Contains(l, id) {
			// remove the preceding cat line if present
			if len(out) > 0 && strings.HasPrefix(strings.TrimSpace(out[len(out)-1]), "cat <<'EOF'") {
				out = out[:len(out)-1]
			}
			skip = true
			continue
		}
		out = append(out, l)
	}
	return os.WriteFile(GrubCustom, []byte(strings.Join(out, "\n")), 0644)
}

// ListGrubEntries parses GrubCustom and returns the names of bootrecov entries.
func ListGrubEntries() ([]string, error) {
	data, err := os.ReadFile(GrubCustom)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var entries []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "menuentry 'Bootrecov ") {
			start := strings.Index(line, "Bootrecov ") + len("Bootrecov ")
			rest := line[start:]
			end := strings.Index(rest, "'")
			if end > -1 {
				path := rest[:end]
				entries = append(entries, filepath.Base(path))
			}
		}
	}
	return entries, nil
}
