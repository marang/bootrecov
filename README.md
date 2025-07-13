# bootrecov

A CLI and TUI-based recovery tool for Arch Linux that creates and manages bootable backups of /boot and generates GRUB entries for fallback booting.

## Features

- Backs up `/boot` to `/boot/efi`
- Lists boot backups and existing GRUB entries
- Safely generates GRUB menu entries
- Remove GRUB entries directly from the TUI
- Styled interface using Bubbletea + Lip Gloss
- Built with Go

## Getting Started

```bash
git clone git@github.com:marang/bootrecov.git
cd bootrecov
go run .
```
