package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func setupDirs(t *testing.T) (string, string, string) {
	t.Helper()
	base := t.TempDir()
	snap := filepath.Join(base, "snapshots")
	efi := filepath.Join(base, "efi")
	grub := filepath.Join(base, "grub")
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(efi, 0o755); err != nil {
		t.Fatal(err)
	}
	return snap, efi, grub
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAddRemoveGrubEntry(t *testing.T) {
	snap, efi, grub := setupDirs(t)
	SnapshotDir = snap
	EfiDir = efi
	GrubCustom = grub

	backup := filepath.Join(efi, "backup1")
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(backup, "vmlinuz-linux"))
	writeFile(t, filepath.Join(backup, "initramfs-linux.img"))

	b := BootBackup{Path: backup, HasKernel: true, HasInitramfs: true}
	if err := AddGrubEntry(b); err != nil {
		t.Fatalf("AddGrubEntry failed: %v", err)
	}

	if _, err := os.Stat(grub); err != nil {
		t.Fatalf("grub file missing: %v", err)
	}
	entries, err := ListGrubEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0] != "backup1" {
		t.Fatalf("unexpected entries: %v", entries)
	}

	if err := RemoveGrubEntry("backup1"); err != nil {
		t.Fatalf("RemoveGrubEntry failed: %v", err)
	}
	entries, err = ListGrubEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entry not removed: %v", entries)
	}
}

func TestDiscoverBackups(t *testing.T) {
	snap, efi, grub := setupDirs(t)
	SnapshotDir = snap
	EfiDir = efi
	GrubCustom = grub

	full := filepath.Join(snap, "full")
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(full, "vmlinuz-linux"))
	writeFile(t, filepath.Join(full, "initramfs-linux.img"))

	partial := filepath.Join(efi, "partial")
	if err := os.MkdirAll(partial, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(partial, "vmlinuz-linux"))

	backups, err := DiscoverBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 2 {
		t.Fatalf("expected 2 backups, got %d", len(backups))
	}
	for _, b := range backups {
		switch filepath.Base(b.Path) {
		case "full":
			if !b.HasKernel || !b.HasInitramfs {
				t.Fatalf("full backup not detected correctly: %#v", b)
			}
		case "partial":
			if !b.HasKernel || b.HasInitramfs {
				t.Fatalf("partial backup not detected correctly: %#v", b)
			}
		default:
			t.Fatalf("unexpected backup: %s", b.Path)
		}
	}
}

func TestTUIEndToEnd(t *testing.T) {
	snap, efi, grub := setupDirs(t)
	SnapshotDir = snap
	EfiDir = efi
	GrubCustom = grub

	backup := filepath.Join(efi, "backup1")
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(backup, "vmlinuz-linux"))
	writeFile(t, filepath.Join(backup, "initramfs-linux.img"))

	m, err := NewModel()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(m.Backups))
	}

	// simulate pressing 'g' to add a GRUB entry then 'q' to quit
	if m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}); m2 != nil {
		m = m2.(Model)
	}
	if m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}); m2 != nil {
		m = m2.(Model)
	}

	entries, err := ListGrubEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0] != "backup1" {
		t.Fatalf("end to end failed, entries: %v", entries)
	}
}
