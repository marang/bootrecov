# Bootrecov Project Specification

This file is the working project contract for contributors and coding agents operating in this repository.
It should describe the repo as it exists now, not as it existed during migration.

## Overview

`bootrecov` is a Linux-only Go utility for managing recovery snapshots of `/boot` and activating selected snapshots as bootloader fallback entries.

The project is aimed at system engineers and advanced Linux users who want:

- bootable fallback entries for previous `/boot` states
- inspectable recovery snapshots outside of full system rollback tools
- explicit bootloader integration instead of opaque recovery automation

The current application exposes both a Cobra CLI and a Bubble Tea TUI.

## Current Architecture

Bootrecov keeps two related storage locations:

- snapshot source: `/var/backups/bootrecov-snapshots/<name>`
- optional active boot mirror for activated snapshots: usually `/boot/efi/bootrecov-snapshots/<name>`, or `/boot/bootrecov-snapshots/<name>` on Fedora/BLS layouts

Important behavior:

- new snapshots are created only in the snapshot source directory
- active boot mirrors exist only for activated bootloader entries
- readiness checks require selected kernel, initramfs, and microcode artifacts to be non-empty regular files with matching snapshot and mirror content; activation resynchronizes damaged existing mirrors or reports an error. This does not prove a real boot will succeed
- snapshot creation and activation require a matching kernel/initramfs filename pair; known different versions are rejected. A later pair change in an active snapshot is not silently published over its existing GRUB/BLS entry; reconcile checks the old entry's referenced artifacts and module tree, restoring the latter from its archive when available. Unknown artifact syntax in an existing recovery entry stops activation and reconcile
- snapshots contain `/boot` state plus an optional compressed SquashFS image of the matching `/usr/lib/modules/<kernel-version>` tree
- kernel identity comes from the selected image or matching versioned artifact filenames; a detected image/filename conflict is unknown, and an unknown image version is never replaced by `uname -r` or an archive filename. Such snapshots remain visible with unknown module status, new activation is rejected, and an existing ambiguous recovery is retained only while its entry paths point to present mirror files. Inactive ambiguous snapshots do not globally block cleanup of unrelated restored modules
- module archives live under `.bootrecov/root-modules/<kernel-version>.sqfs` inside the snapshot source
- module archives are not copied into active boot mirrors
- activation restores an archived `/usr/lib/modules/<kernel-version>` tree automatically when the live root module tree is missing
- activation must not overwrite an existing `/usr/lib/modules/<kernel-version>` tree
- Bootrecov marks newly restored module trees and cleans them up on Arch after their last recovery entry is removed when kernel, package, and ownership checks pass; unsupported distributions and unmarked trees remain untouched
- Arch module cleanup removes matching DKMS builds only for unused, inode-marked restored trees; active recoveries (including Bootrecov's generated `search --file` entries), the running kernel, installed package files, and snapshots without module archives protect their kernel versions
- standard Windows GRUB menuentries targeting the literal `/EFI/Microsoft/Boot/bootmgfw.efi` path with recognized setup commands do not block cleanup; unknown chainloaders, UKIs, dynamic targets, BootNext selections, and unrecognized file searches remain conservative blockers
- entry, snapshot, and module-tree mutations share a cross-process lock under `/run/lock/bootrecov`; module cleanup holds the Arch package lock and is deferred during package transactions
- command stdout and stderr are drained without a line-length limit; retained diagnostics are capped at 256 KiB and TUI progress delivery is best-effort so a slow display cannot stall a child process
- GRUB custom entries are stored in `/etc/grub.d/41_bootrecov_snapshots`
- Fedora-family GRUB/BLS systems use Bootrecov-owned BLS entries under `/boot/loader/entries` when the active mirror is on the same boot filesystem
- GRUB config is regenerated with `grub-mkconfig -o /boot/grub/grub.cfg` after GRUB entry changes
- runtime detection supports Arch, Fedora-family, Ubuntu, and Debian platforms
- boot and ESP path detection uses `/proc/self/mountinfo`, visible boot artifacts, and existing bootloader files while preserving explicit environment overrides
- GRUB is the currently supported bootloader backend
- systemd-boot is detected but not managed yet
- Arch/pacman and Fedora/DNF hooks are implemented; Ubuntu/Debian apt hooks are planned but not implemented
- reconcile removes inactive boot mirrors
- reconcile removes entries for snapshots whose kernel version is known, whose matching root module tree is missing, and whose snapshot has no archived module tree to restore
- reconcile preserves an already-bootable GRUB entry if refresh of its active boot mirror fails transiently
- reconcile leaves an existing active mirror and entry in place if the snapshot boot artifacts are invalid and the mirror still has plausible boot artifacts; otherwise it removes the unusable entry. Either way the backup is reported out of sync, and recovery commands require a verified mirror
- reconcile returns a typed partial failure for per-snapshot mirror removal, module restore, or mirror sync errors, including successful operations and verified final entry/mirror state when available. Later GRUB or entry-list failures retain the completed-operation report and mark final state unverified. Manual CLI returns nonzero; TUI refreshes known state and shows each issue; package post-hooks warn but do not fail the completed transaction. Module cleanup warnings remain distinct

## Implemented Features

- Cobra CLI for backup, bootloader, hook, doctor, reconcile, and TUI entrypoints
- TUI backup browser using Bubble Tea and Lip Gloss
- Backup discovery, metadata inspection, and completeness checks
- Snapshot creation from `/boot`
- Snapshot-side SquashFS archiving of matching root kernel modules when available
- Root module tree compatibility checks and archived module restoration for activated kernel snapshots
- Arch-only ownership-checked cleanup of restored root module trees after recovery entry removal
- active boot mirror activation and deactivation
- GRUB custom and Fedora/BLS entry add, remove, and parse
- Platform and bootloader detection with environment overrides
- Recovery command generation for activated snapshots
- Pacman and DNF hook installation for pre-transaction snapshots and post-transaction active fallback reconciliation
- Arch/mkinitcpio boot-time module restore hook installation for self-restoring GRUB fallback boots
- Fedora/dracut boot-time module restore hook installation for self-restoring GRUB fallback boots
- Rootless QEMU integration test harness under `test/bootvm/`
- Tagged release workflow via GoReleaser
- Tagged AUR publish workflow using `PKGBUILD`

## Non-Interactive Commands

The binary currently supports:

- `bootrecov`
  Starts the TUI.
- `bootrecov tui`
  Starts the TUI explicitly.
- `bootrecov doctor`
  Shows detected platform, bootloader, support state, and active paths.
- `bootrecov backup list|create|activate|deactivate|delete|recovery`
  Manages snapshots from the CLI.
- `bootrecov bootloader list|activate|deactivate|recovery`
  Manages active bootloader recovery entries.
- `bootrecov grub list`
  Deprecated compatibility alias for `bootrecov bootloader list`.
- `bootrecov reconcile`
  Reconciles active boot mirrors and GRUB state.
- `bootrecov hook install [absolute-binary-path]`
  Installs or refreshes the platform package-manager hook. Currently implemented for Arch/pacman and Fedora/DNF.
- `bootrecov hook uninstall`
  Removes the platform package-manager hook when present.

Compatibility aliases retained:

- `bootrecov backup-now`
- `bootrecov install-pacman-hook [absolute-binary-path]`
- `bootrecov recovery-commands <snapshot-name>`

These commands are implemented in [`cmd/bootrecov/main.go`](cmd/bootrecov/main.go).

Every TUI or CLI invocation requires explicit per-run risk acknowledgement. Interactive runs prompt for `y/N` confirmation; non-interactive automation must pass `--yes-i-understand` or set `BOOTRECOV_ACCEPT_RISK=1`.

## TUI Controls

Backups view:

- `b`: create snapshot
- `g`: toggle EFI + bootloader activation
- `s`: reconcile active boot mirrors and GRUB state
- `r`: show GRUB recovery commands for selected backup
- `p`: install package-manager hook
- `d`: delete selected backup, with confirmation
- `tab`: switch to GRUB entries
- `q`: quit

Bootloader entries view:

- `x`: remove selected GRUB entry
- `tab`: switch back to backups
- `q`: quit

## Dependency Model

Runtime assumptions:

- Linux
- GRUB for boot entry management
- EFI system layout
- `rclone`
- `grub-mkconfig`
- `mksquashfs` and `unsquashfs` from Arch package `squashfs-tools`
- `file` on Arch for identifying primary kernel versions during restored module cleanup

The TUI performs a startup dependency check and exits early with a clear error if required runtime tools are missing.

Normal operation typically requires elevated privileges because the app writes to:

- `/var/backups/bootrecov-snapshots`
- `/boot/efi/bootrecov-snapshots`
- `/boot/bootrecov-snapshots` on Fedora/BLS systems
- `/usr/lib/modules/<kernel-version>` when restoring a missing archived module tree
- `/etc/grub.d/41_bootrecov_snapshots`
- `/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook` and `/etc/pacman.d/hooks/96-bootrecov-post-transaction.hook` on Arch
- `/usr/lib/initcpio/install/bootrecov`, `/usr/lib/initcpio/hooks/bootrecov`, and `/etc/mkinitcpio.conf` on Arch/mkinitcpio
  These hooks are created by explicit opt-in and removed by `bootrecov hook uninstall` or by the package removal script when uninstalling the Arch package. Removal also drops `bootrecov` from `HOOKS=(...)` and regenerates initramfs images when `mkinitcpio` is available.

Environment overrides:

- `BOOTRECOV_PLATFORM=arch|fedora|ubuntu|debian`
- `BOOTRECOV_BOOTLOADER=grub|systemd-boot`
- `BOOTRECOV_BOOT_DIR=/boot`
- `BOOTRECOV_ESP_DIR=/boot/efi`
- `BOOTRECOV_EFI_MIRROR_DIR=/boot/efi/bootrecov-snapshots`
- `BOOTRECOV_ROOT_MODULES_DIR=/usr/lib/modules`
- `BOOTRECOV_PACMAN_HOOK_PATH=/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook`
- `BOOTRECOV_PACMAN_POST_HOOK_PATH=/etc/pacman.d/hooks/96-bootrecov-post-transaction.hook`
- `BOOTRECOV_MKINITCPIO_CONF=/etc/mkinitcpio.conf`
- `BOOTRECOV_MKINITCPIO_INSTALL_HOOK=/usr/lib/initcpio/install/bootrecov`
- `BOOTRECOV_MKINITCPIO_RUNTIME_HOOK=/usr/lib/initcpio/hooks/bootrecov`
- `BOOTRECOV_MKINITCPIO_BIN=mkinitcpio`
- `BOOTRECOV_GRUB_MKCONFIG=grub-mkconfig`
- `BOOTRECOV_BLS_ENTRIES_DIR=/boot/loader/entries`
- `BOOTRECOV_DNF5_ACTIONS_PATH=/etc/dnf/libdnf5-plugins/actions.d/95-bootrecov.actions`
- `BOOTRECOV_DNF4_PRE_ACTIONS_PATH=/etc/dnf/plugins/pre-transaction-actions.d/95-bootrecov.action`
- `BOOTRECOV_DNF4_POST_ACTIONS_PATH=/etc/dnf/plugins/post-transaction-actions.d/95-bootrecov.action`
- `BOOTRECOV_DRACUT_MODULE_DIR=/usr/lib/dracut/modules.d/95bootrecov`
- `BOOTRECOV_DRACUT_BIN=dracut`

Path detection should handle common `/boot/efi`, `/efi`, and ESP-at-`/boot` layouts conservatively. Fedora-family BLS layouts should default active mirrors to `/boot/bootrecov-snapshots` when BLS entries are present and no explicit mirror override is set. Explicit environment overrides always take precedence.
Arch/mkinitcpio hook path detection should use the `mkinitcpio` binary from `PATH`, existing mkinitcpio config, and existing initcpio hook directories before falling back to defaults. Do not apply Arch/mkinitcpio paths to other initramfs backends.
Fedora-family hook installation uses DNF action plugin directories only when present, prefers DNF5 over DNF4 when both are installed, scopes actions to boot-critical package filters, and installs a dracut module for boot-time restore.
If multiple bootloader signals are detected, report ambiguity and require/accept `BOOTRECOV_BOOTLOADER` to choose the intended backend instead of guessing.

## Backup Profiles

Environment variable:

- `BOOTRECOV_BACKUP_PROFILE=full`
- `BOOTRECOV_BACKUP_PROFILE=minimal`

`full` copies the `/boot` tree while excluding the mounted ESP subtree such as `/boot/efi/**`, so firmware files are not duplicated into snapshots or active boot mirrors.

`minimal` currently includes:

- `vmlinuz*`
- `initrd.img*`
- `initramfs*.img`
- `intel-ucode.img`
- `amd-ucode.img`
- `grub/**`

## Package Hooks

Installed Arch hook paths:

- `/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook`
- `/etc/pacman.d/hooks/96-bootrecov-post-transaction.hook`
- `/usr/lib/initcpio/install/bootrecov`
- `/usr/lib/initcpio/hooks/bootrecov`
- `/etc/mkinitcpio.conf` is updated to include `bootrecov` in `HOOKS=(...)`

Current trigger set:

- `linux*`
- `grub`
- `mkinitcpio`
- `systemd`

Current Arch action:

- run `/usr/bin/env BOOTRECOV_ACCEPT_RISK=1 bootrecov hook backup-now` before the transaction
- run `/usr/bin/env BOOTRECOV_ACCEPT_RISK=1 bootrecov hook reconcile-active` after the transaction to restore archived modules for already-active fallback kernels and refresh EFI/GRUB state
- hook-created snapshots are not activated in EFI or the bootloader automatically
- if snapshot space is insufficient, the hook prints a warning and exits successfully so the package transaction is not blocked
- non-space pre-transaction errors still fail the hook
- post-transaction reconcile errors are printed as warnings and do not fail the completed package transaction
- mkinitcpio boot-time restore runs as a late hook after root is mounted at `/new_root`; it extracts archived modules into `/new_root/<configured-root-modules-dir>/<kernel-version>`, normally `/new_root/usr/lib/modules/<kernel-version>`, only for Bootrecov GRUB fallback boots
- Fedora/dracut boot-time restore runs as a pre-pivot dracut hook after root is mounted at `/sysroot`; it recognizes either Bootrecov's kernel marker or a `BOOT_IMAGE` path under `bootrecov-snapshots`; initramfs-tools support is planned but not implemented

Ubuntu/Debian apt/dpkg hooks are planned but not implemented.

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
make test-bootvm-arch-grub-cleanup
make test-bootvm-ubuntu-grub
make test-bootvm-debian-grub
make test-bootvm-fedora-grub-bls
make test-bootvm-grub-matrix
make test-bootvm-platform-matrix
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
  backup discovery, copy logic, EFI sync, hook generation
- `internal/tui/grub.go`
  GRUB entry parsing, generation, recovery commands, and config regeneration
- `internal/tui/mountinfo.go`
  Linux mountinfo parsing and mountpoint lookup helpers
- `internal/tui/platform.go`
  platform and bootloader detection, active layout, and support checks
- `internal/tui/model.go`
  Bubble Tea model, key handling, status flow
- `internal/tui/view.go`
  view helper placeholder
- `test/bootvm/`
  rootless QEMU integration harness and related scripts
- `docs/roadmap/`
  active roadmap for distro support, bootloader support, testing gates, and release gates
- `plan/bootrecov-recovery-platform-roadmap.md`
  compatibility pointer to `docs/roadmap/`
- `docker-compose.yml`
  legacy privileged/container-based boot test path kept for reference

## Contributor Expectations

- Keep `README.md` and `AGENTS.md` aligned with actual repo behavior.
- Keep `docs/roadmap/` aligned with support status when changing distro or bootloader behavior.
- Prefer updating tests together with behavior changes.
- Do not describe future ideas as current features.
- Treat bootloader entry safety and recovery availability as high-priority correctness concerns.
- Preserve Linux-only assumptions unless the repo is explicitly redesigned.
- Before opening a PR, ensure `go test ./...` passes at minimum.

## License

License: MIT

Current repo license file:

- [`LICENSE`](LICENSE)
