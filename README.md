# bootrecov

A CLI and TUI-based recovery tool for Arch Linux that creates and manages bootable backups of /boot and generates GRUB entries for fallback booting.

## Features

- Backs up `/boot` to `/boot/efi`
- Lists boot backups and existing GRUB entries
- Safely generates GRUB menu entries
- Remove GRUB entries directly from the TUI
- Styled interface using Bubbletea + Lip Gloss
- Uses `/etc/grub.d/41_custom_boot_backups` for custom entries
- Built with Go

The custom GRUB file is a Bash script that outputs entries using `cat <<'EOF'` blocks. Bootrecov will create the file with the proper header if it doesn't exist.

## Getting Started

```bash
git clone git@github.com:marang/bootrecov.git
cd bootrecov
go run .
```
