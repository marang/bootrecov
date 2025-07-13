package tui

import (
	"bufio"
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
	f, err := os.OpenFile(GrubCustom, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	entry := fmt.Sprintf("menuentry 'Bootrecov %s' --id bootrecov-%s {\n"+
		"    search --file --set=root %s/vmlinuz-linux\n"+
		"    linux %s/vmlinuz-linux root=UUID=your-root fsck.mode=skip ro\n"+
		"    initrd %s/initramfs-linux.img\n"+
		"}\n",
		b.Path, filepath.Base(b.Path), b.Path, b.Path, b.Path)

	if _, err := f.WriteString(entry); err != nil {
		return err
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
		if strings.Contains(l, id) {
			skip = true
			continue
		}
		if skip {
			if strings.Contains(l, "}") {
				skip = false
			}
			continue
		}
		out = append(out, l)
	}
	return os.WriteFile(GrubCustom, []byte(strings.Join(out, "\n")), 0644)
}
