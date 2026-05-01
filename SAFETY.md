# Safety Model

Bootrecov is intentionally conservative because it writes to boot-critical locations.

## Default Safety Properties

- New snapshots are created in `/var/backups/bootrecov-snapshots`, not directly in EFI.
- EFI mirrors are created only when a snapshot is explicitly activated.
- Activation validates snapshot names before path-sensitive operations.
- Activation verifies that the EFI root is mounted before copying files.
- Activation refuses a snapshot when the kernel version is known and `/usr/lib/modules/<version>` is missing.
- Activation does not write to `/usr/lib/modules`.
- Internal `.bootrecov` metadata is excluded from EFI mirrors.
- Reconcile removes inactive EFI mirrors but preserves an already bootable active GRUB entry if refreshing its EFI mirror fails transiently.
- There is no automatic pruning, so Bootrecov does not delete older snapshots without an explicit delete command.

## High-Risk Paths

Normal use may write to:

- `/var/backups/bootrecov-snapshots`
- `/boot/efi/bootrecov-snapshots`
- `/etc/grub.d/41_bootrecov_snapshots`
- `/boot/grub/grub.cfg`
- `/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook`

## Recommended Use

- Keep a rescue USB available.
- Test the full create, activate, reboot, deactivate flow in a VM or spare system first.
- Keep known-good system backups outside Bootrecov.
- Do not enable the pacman hook without monitoring snapshot disk usage.
- Keep at least one known-good kernel and matching `/usr/lib/modules/<version>` tree installed.

## Known Non-Goals

- Bootrecov does not repair a broken root filesystem.
- Bootrecov does not automatically mount or restore archived module SquashFS images.
- Bootrecov does not detect failed boots automatically.
- Bootrecov does not prune snapshots automatically.
