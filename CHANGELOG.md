# Changelog

## v0.4.0 - 2026-05-01

### Added

- Roadmap documentation under `docs/roadmap/` for distribution support, bootloader support, testing gates, and release gates.
- Hook-specific `bootrecov hook backup-now` entrypoint for pre-transaction snapshots.

### Changed

- Pacman hooks now skip pre-transaction snapshots with a warning when space is insufficient, instead of blocking the package transaction.
- Pacman hooks still do not automatically activate created snapshots in EFI or the bootloader.
- Error handling now exposes typed errors for backup, EFI, bootloader, dependency, sync, and space failures instead of requiring message-string matching.

## v0.3.0 - 2026-05-01

### Added

- Platform and bootloader detection with `bootrecov doctor`.
- Conservative boot directory, ESP root, and GRUB config path detection for common non-default partition layouts.
- Generic `bootrecov bootloader ...` commands backed by the current GRUB implementation.
- Per-invocation risk acknowledgement for TUI and CLI usage.
- Environment overrides for platform, bootloader, boot directory, ESP directory, and EFI mirror directory.
- Initial Ubuntu/Debian detection for GRUB-based layouts.
- systemd-boot detection as an explicit unsupported backend instead of silently assuming GRUB.

### Changed

- `bootrecov grub list` is now a deprecated compatibility alias for `bootrecov bootloader list`.
- Runtime checks reject unsupported bootloader mutations before touching EFI or bootloader configuration.
- Package-manager hook installation now reports unsupported non-Arch platforms explicitly.
- Ambiguous GRUB and systemd-boot signals are rejected instead of silently choosing a backend.
- GRUB entry management and mountinfo parsing are split out of the core backup module.

## v0.2.0 - 2026-05-01

### Added

- Compressed SquashFS archives for matching `/usr/lib/modules/<kernel-version>` trees during snapshot creation.
- Root module readiness status in CLI and TUI backup listings.
- Activation safety check for missing root module trees.
- EFI mount verification before snapshot activation.
- Central snapshot-name validation for path-sensitive operations.
- Rootless VM E2E coverage for SquashFS archives, missing old-kernel modules, GRUB boot, and booting after primary kernel corruption.
- Watch-mode VM pane with runner activity and setup progress logs.

### Changed

- EFI mirror sync excludes internal `.bootrecov` metadata.
- AUR packaging includes `squashfs-tools`.
- AUR build uses an isolated Go module cache and `go mod download`.
- Make targets use stable temporary Go caches by default.
- `rclone` feature detection handles alias-style help output such as `-l, --links`.

### Fixed

- Prevented path traversal through malicious snapshot names.
- Closed the GRUB custom file before running `grub-mkconfig`, avoiding `Text file busy`.
- Avoided treating archived module images as sufficient for activation when root modules are missing.
