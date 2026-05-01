# Changelog

## v0.2.0 - 2026-05-01

### Added

- Compressed SquashFS archives for matching `/usr/lib/modules/<kernel-version>` trees during snapshot creation.
- Root module readiness status in CLI and TUI backup listings.
- Activation safety check for missing root module trees.
- EFI mount verification before snapshot activation.
- Central snapshot-name validation for path-sensitive operations.
- Rootless VM E2E coverage for SquashFS archives, missing old-kernel modules, GRUB boot, and booting after primary kernel corruption.
- Watch-mode VM pane with runner activity and setup progress logs.
- Review documentation under `.docs/review/`.

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
