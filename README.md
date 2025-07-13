# bootrecov

A CLI and TUI-based recovery tool for Arch Linux that creates and manages bootable backups of /boot and generates GRUB entries for fallback booting.

## Features

- Backs up `/boot` to `/boot/efi`
- Lists boot backups
- Safely generates GRUB menu entries
- Built with Go + Bubbletea

## Getting Started

```bash
git clone git@github.com:marang/bootrecov.git
cd bootrecov
go run .
```
