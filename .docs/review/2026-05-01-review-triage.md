# Review Triage

Date: 2026-05-01

## Summary

The two external reviews were useful. The highest-risk security findings have been addressed in code and covered by tests. The product caution from the first review still mostly stands: Bootrecov is useful, but it touches boot-critical paths and should be tested in a VM or spare system before relying on it.

## Findings Status

| Finding | Status | Notes |
| --- | --- | --- |
| Snapshot-name path traversal | Fixed | Added central `validateBackupName` and applied it to activation, deactivation, deletion, recovery commands, and GRUB entry creation. |
| EFI mount not verified | Fixed | Activation now verifies the EFI root mount before copying a snapshot into EFI. |
| `rclone --links` detection misses alias lines | Fixed | `helpMentionsFlag` now detects flags in alias forms such as `-l, --links`. |
| AUR missing `squashfs-tools` dependency | Fixed | `PKGBUILD` and AUR workflow include `squashfs-tools`. |
| AUR Go module handling | Improved | `PKGBUILD` now runs `go mod download` with an isolated `GOMODCACHE` and builds with `-mod=readonly`. Fully vendored/offline module sources remain a future packaging hardening step if needed. |
| No pruning | Open | Still intentionally not implemented. Users must manage snapshot count/storage. |
| Project maturity/community signal | Open | This is external adoption state, not a code fix. |

## Test Coverage Added Or Exercised

- Invalid/path-traversal snapshot names are rejected.
- Activation requires a mounted EFI root when mount enforcement is enabled.
- Activation succeeds when the configured EFI root is represented as a mount point.
- `rclone` help parsing detects `-l, --links`.
- VM E2E covers SquashFS module archive creation, EFI mirror metadata exclusion, missing old-kernel module safety, GRUB boot, and booting the backup after corrupting the primary kernel.

## Remaining Follow-Up Candidates

- Automatic pruning policy for snapshots.
- Optional fully vendored/offline AUR packaging mode.
- More explicit release documentation around checksum updates for real AUR tags.
- More distro-specific documentation for non-Arch GRUB layouts.
