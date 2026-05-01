# Bootrecov Project Specification

This file is the working project contract for contributors and coding agents operating in this repository.
It should describe the repo as it exists now, not as it existed during migration.

## Overview

`bootrecov` is a Linux-only Go utility for managing recovery snapshots of `/boot` and activating selected snapshots as GRUB fallback entries.

The project is aimed at system engineers and advanced Linux users who want:

- bootable fallback entries for previous `/boot` states
- inspectable recovery snapshots outside of full system rollback tools
- explicit GRUB integration instead of opaque recovery automation

The current application exposes both a Cobra CLI and a Bubble Tea TUI.

## Current Architecture

Bootrecov keeps two related storage locations:

- snapshot source: `/var/backups/bootrecov-snapshots/<name>`
- optional EFI mirror for activated snapshots: `/boot/efi/bootrecov-snapshots/<name>`

Important behavior:

- new snapshots are created only in the snapshot source directory
- EFI mirrors exist only for activated GRUB entries
- snapshots contain `/boot` state plus an optional compressed SquashFS image of the matching `/usr/lib/modules/<kernel-version>` tree
- module archives live under `.bootrecov/root-modules/<kernel-version>.sqfs` inside the snapshot source
- module archives are not copied into EFI mirrors
- activation must not write to `/usr/lib/modules` automatically
- older kernel fallback entries require the matching `/usr/lib/modules/<kernel-version>` tree to still exist on the root filesystem
- GRUB custom entries are stored in `/etc/grub.d/41_bootrecov_snapshots`
- GRUB config is regenerated with `grub-mkconfig -o /boot/grub/grub.cfg` after GRUB entry changes
- reconcile removes inactive EFI mirrors
- reconcile removes entries for snapshots whose kernel version is known but whose matching root module tree is missing
- reconcile preserves an already-bootable GRUB entry if refresh of its active EFI mirror fails transiently

## Implemented Features

- Cobra CLI for backup, GRUB, hook, reconcile, and TUI entrypoints
- TUI backup browser using Bubble Tea and Lip Gloss
- Backup discovery, metadata inspection, and completeness checks
- Snapshot creation from `/boot`
- Snapshot-side SquashFS archiving of matching root kernel modules when available
- Root module tree compatibility checks for activated kernel snapshots
- EFI mirror activation and deactivation
- GRUB entry add, remove, and parse
- Recovery command generation for activated snapshots
- Pacman hook installation for pre-transaction snapshots
- Rootless QEMU integration test harness under `test/bootvm/`
- Tagged release workflow via GoReleaser
- Tagged AUR publish workflow using `PKGBUILD`

## Non-Interactive Commands

The binary currently supports:

- `bootrecov`
  Starts the TUI.
- `bootrecov tui`
  Starts the TUI explicitly.
- `bootrecov backup list|create|activate|deactivate|delete|recovery`
  Manages snapshots from the CLI.
- `bootrecov grub list`
  Lists Bootrecov GRUB entries.
- `bootrecov reconcile`
  Reconciles EFI mirrors and GRUB state.
- `bootrecov hook install [absolute-binary-path]`
  Installs or refreshes the pacman hook.

Compatibility aliases retained:

- `bootrecov backup-now`
- `bootrecov install-pacman-hook [absolute-binary-path]`
- `bootrecov recovery-commands <snapshot-name>`

These commands are implemented in [`cmd/bootrecov/main.go`](cmd/bootrecov/main.go).

## TUI Controls

Backups view:

- `b`: create snapshot
- `g`: toggle EFI + GRUB activation
- `s`: reconcile EFI mirrors and GRUB state
- `r`: show GRUB recovery commands for selected backup
- `p`: install pacman hook
- `d`: delete selected backup, with confirmation
- `tab`: switch to GRUB entries
- `q`: quit

GRUB entries view:

- `x`: remove selected GRUB entry
- `tab`: switch back to backups
- `q`: quit

## Dependency Model

Runtime assumptions:

- Linux
- GRUB
- EFI system layout
- `rclone`
- `grub-mkconfig`
- `mksquashfs` from Arch package `squashfs-tools`

The TUI performs a startup dependency check and exits early with a clear error if required runtime tools are missing.

Normal operation typically requires elevated privileges because the app writes to:

- `/var/backups/bootrecov-snapshots`
- `/boot/efi/bootrecov-snapshots`
- `/etc/grub.d/41_bootrecov_snapshots`
- `/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook`

## Backup Profiles

Environment variable:

- `BOOTRECOV_BACKUP_PROFILE=full`
- `BOOTRECOV_BACKUP_PROFILE=minimal`

`full` copies the `/boot` tree while excluding the mounted ESP subtree such as `/boot/efi/**`, so firmware files are not duplicated into snapshots or active EFI mirrors.

`minimal` currently includes:

- `vmlinuz*`
- `initrd.img*`
- `initramfs*.img`
- `intel-ucode.img`
- `amd-ucode.img`
- `grub/**`

## Pacman Hook

Installed hook path:

- `/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook`

Current trigger set:

- `linux*`
- `grub`
- `mkinitcpio`
- `systemd`

Current action:

- run `bootrecov backup-now` before the transaction

## Current Non-Goals

These are not implemented and should not be described as current behavior:

- automatic pruning of old snapshots
- chroot or repair-shell workflows
- full root filesystem or package rollback
- boot failure detection from journald or reboot history
- non-Linux support
- release artifacts for `darwin` or `windows`

## Build, Test, And Release

Build and local execution:

```bash
make build
make run
```

Formatting and validation:

```bash
make fmt
go vet ./...
go test ./...
make test
```

Rootless integration test:

```bash
make test-bootvm-requirements
make test-bootvm
make test-bootvm-watch
```

CI:

- [`.github/workflows/go-tests.yml`](.github/workflows/go-tests.yml) runs `make test`

Release automation:

- [`.github/workflows/release.yml`](.github/workflows/release.yml)
- [`.goreleaser.yml`](.goreleaser.yml)

AUR automation:

- [`.github/workflows/aur.yml`](.github/workflows/aur.yml)
- [`PKGBUILD`](PKGBUILD)

Release targets are Linux-only:

- `linux/amd64`
- `linux/arm64`

## Release Discipline

Before creating a release tag:

- ensure the working tree is clean
- ensure release-critical files are actually tracked by git, not just present in the working tree
- verify the CLI entrypoint exists in git history where expected
- run `go test ./...`
- run `go vet ./...`
- confirm the release configuration matches the current repository layout

Do not:

- cut tags from a dirty tree
- mix release fixes with unrelated roadmap or documentation work right before tagging
- assume a local successful build proves the tagged git tree is complete
- reintroduce ignore rules that can hide tracked source directories such as `cmd/bootrecov/`

## Repository Structure

- `cmd/bootrecov/main.go`
  CLI entry point and non-interactive command dispatch
- `internal/tui/backups.go`
  backup discovery, copy logic, EFI sync, GRUB integration, hook generation
- `internal/tui/model.go`
  Bubble Tea model, key handling, status flow
- `internal/tui/view.go`
  view helper placeholder
- `test/bootvm/`
  rootless QEMU integration harness and related scripts
- `docker-compose.yml`
  legacy privileged/container-based boot test path kept for reference

## Contributor Expectations

- Keep `README.md` and `AGENTS.md` aligned with actual repo behavior.
- Prefer updating tests together with behavior changes.
- Do not describe future ideas as current features.
- Treat GRUB entry safety and recovery availability as high-priority correctness concerns.
- Preserve Linux-only assumptions unless the repo is explicitly redesigned.
- Before opening a PR, ensure `go test ./...` passes at minimum.

## License

License: MIT

Current repo license file:

- [`LICENSE`](LICENSE)
