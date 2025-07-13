# Bootrecov Project Specification (Codex)

## Overview
Bootrecov is a Go-based CLI and TUI application designed for Linux system engineers and power users who want reliable, inspectable boot recovery options when a system update breaks kernel or boot loader configurations.

The project was originally implemented as a Bash script to:
- Backup `/boot` to both `/var/backups/boot-snapshots` and `/boot/efi/boot-backups`
- Register a pacman hook to trigger backups before kernel/GRUB updates
- Generate GRUB entries pointing to these backups for manual boot recovery

Now being migrated to Go with Bubbletea for a full-featured interactive terminal interface.

---

## Key Goals
- Allow users to safely **boot into a previous /boot state**
- Enable full inspection of broken `/boot` while booted from a backup
- Provide a TUI for browsing, validating, and managing boot backups
- Support GRUB custom menu entry generation and removal
- Keep recovery and diagnostics decoupled from full rollback tools (e.g., snapper)

---

## Current System Architecture

### Bash Script (legacy)
- `/usr/local/bin/snap_and_backup_boot`: Main entry
- Creates timestamped copies of `/boot` in two locations
- Pacman hook at `/etc/pacman.d/hooks/boot-backup.hook`
- EFI recovery GRUB entries generated in `/etc/grub.d/41_custom_boot_backups`

---

## Go Application (bootrecov)

### Features to implement:

#### ✅ Backup Listing
- List backups from:
  - `/var/backups/boot-snapshots/*`
  - `/boot/efi/boot-backups/*`
- Show creation time, completeness (kernel/initramfs present)

#### ✅ GRUB Entry Management
- Parse `/etc/grub.d/41_custom_boot_backups`
- Safely append/remove entries for backups
- Ensure entries are never overwritten by default `grub-mkconfig`

#### ✅ TUI (Bubbletea)
- Navigate backup list interactively
- Display GRUB boot entry status
- Flag backups with missing files (e.g. missing `initramfs-linux.img`)
- Mark backups already listed in GRUB

#### ✅ Manual Recovery Hints
- Generate boot commands for GRUB rescue shell
- Show `linux`, `initrd`, and `search` commands per backup

#### 🔜 Future Ideas
- Integrate optional mount + chroot to debug broken `/boot`
- Add support for auto-pruning old backups
- Detect failed boots via journald or `last -x reboot`
- Export to USB boot media

---

## Tech Stack
- Language: Go (>=1.20)
- UI: Bubbletea + Lipgloss + Bubbles
- Filesystem: os, filepath, ioutil, text/template
- No Snapper, no DBus

---

## GitHub
**Repo:** `git@github.com:marang/bootrecov.git`

## License
**MIT License** — permissive, simple, aligns with Go CLI and Bubbletea ecosystem.

## Installation (Development)
```bash
git clone git@github.com:marang/bootrecov.git
cd bootrecov
go mod init github.com/marang/bootrecov
go get github.com/charmbracelet/bubbletea
go run .
```

---

## Next Milestone
- [ ] Scaffold `tui/model.go` and `tui/view.go`
- [ ] Display list of EFI backups with kernel/initramfs check
- [ ] Indicate if GRUB entry exists for each backup
- [ ] Generate/Remove GRUB entry from UI selection

