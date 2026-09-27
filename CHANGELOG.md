# Changelog

## Unreleased

### Added

- `bootrecov doctor` now groups core, path, GRUB, and platform-specific diagnostics with color-coded status values; `NO_COLOR=1` disables colors for automation.
- Fedora/RHEL-family detection with Fedora-specific hook paths in `doctor`.
- Fedora DNF5/DNF4 action hook installation and Fedora dracut boot-time module restore hook installation.
- Fedora/BLS Bootrecov entries are preferred when the active snapshot mirror is on the same boot filesystem.
- Fedora GRUB/BLS rootless VM gate with DNF/dracut, BLS, and reboot coverage.
- README installation notes for the Arch/AUR `yay -S bootrecov` flow.

### Fixed

- `bootrecov doctor` no longer prints Fedora/dracut diagnostics on Arch, and now reports distro-specific tool availability such as `dracut-bin available` on Fedora.
- Fedora/BLS entries now keep Btrfs `/boot` mount-root paths visible to GRUB.
- Fedora/BLS systems now default active mirrors to `/boot/bootrecov-snapshots` without requiring a VM-only override.
- Fedora DNF action hooks are now scoped to boot-critical package filters and prefer DNF5 over DNF4 when both plugin layouts exist.
- Entries-only systemd-boot detection is preserved on non-Fedora systems while Fedora GRUB/BLS entries no longer create false ambiguity.
- The rootless VM harness no longer exits early under `set -euo pipefail` when cached build artifacts are already current.
- Fedora dracut restore now recognizes Bootrecov fallback boots from `BOOT_IMAGE` paths when GRUB/BLS does not preserve custom kernel markers.
- Debian VM gate now finds GRUB tools installed under `/usr/sbin` in non-login SSH commands.

## v0.4.13 - 2026-09-27

### Fixed

- Recognized Windows GRUB chainloader entries no longer block Arch cleanup of unused Bootrecov-restored kernel modules and matching DKMS builds.
- Bootrecov's generated `search --file` recovery entries protect their own kernel versions and visible matching images on other mounted filesystems without blocking cleanup of a different unused kernel.
- GRUB paths now correctly include the root filesystem and Btrfs mount roots.
- Standard GRUB header assignments, EFI class labels, and platform comparisons no longer cause false cleanup warnings. Unknown chainloaders, UKIs, dynamic targets, and BootNext selections remain conservative blockers.
- Shared recovery kernels, the running kernel, installed kernel packages, and unmarked module trees remain protected; package/process locks and mount checks are preserved.

### Added

- Regression coverage through actual activation, deactivation, deletion, and entry-removal paths.
- `make test-bootvm-arch-grub-cleanup`: a disposable Arch GRUB recovery boot with real kernel packages and DKMS builds, including shared kernels, unknown loaders, locks, mounts, and ownership checks.

## v0.4.12 - 2026-09-27

### Fixed

- Command output is drained before process cleanup closes its pipes, preventing lost progress and diagnostic lines from short-lived commands. The streaming regression test now exercises single-processor scheduling.
- Includes the restored-module cleanup changes below. The v0.4.11 GitHub release was not published because CI exposed this output race.

## v0.4.11 - 2026-09-27

### Fixed

- On Arch, unused module trees restored by Bootrecov can be cleaned up after recovery entry removal, deactivation, deletion, or reconciliation. Ownership markers, running kernels, installed packages, and boot references protect trees that must remain.
- Recovery mutations are serialized across processes; cleanup holds the pacman database lock and defers during package transactions.
- GRUB configuration sources and selected mounted boot filesystems are inspected conservatively. Unresolved references, `chainloader` entries (including Windows), and `search --file` defer module cleanup.
- The TUI and package hook distinguish completed recovery changes from subsequent cleanup warnings and return current entry and module status.
- Arch packaging and startup checks include the `file` dependency; other platforms do not require it for this Arch-only cleanup.
- The Ubuntu/Debian VM gate reads the package-hook diagnostic correctly when its status contains multiple words.

## v0.4.6 - 2026-05-20

### Changed

- Backup listings now distinguish restorable snapshots from incomplete snapshots.
- CLI `backup list` now shows a `RESTORABLE` column for active snapshots that can restore archived modules before boot.

## v0.4.5 - 2026-05-15

### Changed

- Activation now restores archived `/usr/lib/modules/<kernel-version>` trees automatically when the live module tree is missing.
- Snapshot creation now stages backups and only publishes them after required boot artifacts and module archives are verified.
- Rclone snapshot sync now uses metadata preservation when supported so local file modes are retained.

### Fixed

- Failed or unreadable `/boot` copies no longer leave partial snapshots visible as normal backups.
- Reconcile now restores archived modules for active entries instead of removing entries only because the live module tree is missing.

## v0.4.2 - 2026-05-01

### Changed

- README and roadmap now distinguish Ubuntu/Debian GRUB gate availability from apt/dpkg hook support.
- Rootless VM testing now has explicit Ubuntu+GRUB, Debian+GRUB, and combined GRUB matrix targets.

### Fixed

- AUR builds now make the isolated Go module cache writable again after dependency download/build so `makepkg` clean-build removal can delete the old `srcdir`.

## v0.4.1 - 2026-05-01

### Changed

- Interactive risk acknowledgement now uses a bordered warning panel with a `y/N` confirmation instead of requiring a typed phrase.

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
