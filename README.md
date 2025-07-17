# bootrecov

Bootrecov keeps bootable copies of `/boot` and can add them to GRUB so you can boot an older kernel when things break.
It is written in Go with a simple TUI.

## Who is it for?

- Arch Linux and other pacman based systems
- Anyone using GRUB and an EFI boot setup
- Users comfortable with the command line

## Requirements

- Linux with GRUB (tested on Arch)
- Pacman package manager for the backup hook
- Go **1.20+** to build from source

## Getting Started

```bash
git clone https://github.com/marang/bootrecov.git
cd bootrecov
go run .
```

The interface lists boot backups found under `/boot/efi/boot-backups`. Press **g** to add or remove a GRUB entry and **tab** to switch views.

## Features

- Backs up `/boot` to `/boot/efi`
- Lists boot backups and existing GRUB entries
- Safely generates GRUB menu entries
- Remove GRUB entries directly from the TUI
- Styled interface using Bubbletea + Lip Gloss
- Uses `/etc/grub.d/41_custom_boot_backups` for custom entries
- Built with Go

The custom GRUB file is a Bash script that outputs entries using `cat <<'EOF'` blocks. Bootrecov creates the file with the proper header if it doesn't exist.

## Automated Boot Test with Docker or Podman Compose

A Compose setup is provided to build an Arch Linux VM with QEMU and boot it using **bootrecov**. This requires a host capable of running privileged containers and KVM acceleration.

Run with Docker Compose:

```bash
docker compose up
```

Or with Podman Compose:

```bash
podman compose up
```

The service installs QEMU and Arch tools, builds the project, creates a small UEFI disk image, installs GRUB and **bootrecov**, then launches QEMU. You can select the generated Bootrecov entry from the GRUB menu to verify the system boots correctly.

All files for this test live under the `docker/` directory.

## Continuous Integration

A CircleCI workflow runs `go test ./...` to verify the project builds and tests pass.

A GitHub Actions workflow in `.github/workflows/vm-test.yml` can run the QEMU
boot test automatically. It requires a self-hosted runner with Docker and KVM
so the `docker compose` service can launch the VM.

