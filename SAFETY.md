# Safety Model

Bootrecov is intentionally conservative because it writes to boot-critical locations.

## Default Safety Properties

- New snapshots are created in `/var/backups/bootrecov-snapshots`, not directly in a boot mirror.
- Active boot mirrors are created only when a snapshot is explicitly activated.
- Activation validates snapshot names before path-sensitive operations.
- Activation verifies that the configured boot mirror root is mounted before copying files.
- Unsupported bootloaders are detected but rejected before bootloader mutations.
- Ambiguous bootloader signals are rejected instead of enabling a potentially wrong backend.
- ESP auto-detection accepts only common ESP mount paths or FAT mounts with bootloader markers.
- Unsupported package hook platforms return an explicit error instead of installing partial hooks.
- TUI and CLI invocations require explicit risk acknowledgement via prompt, flag, or environment variable.
- Activation restores an archived `/usr/lib/modules/<version>` tree when the live tree is missing and the snapshot contains a matching SquashFS archive.
- Activation and reconcile do not overwrite an existing `/usr/lib/modules/<version>` tree.
- Activation requires an identifiable kernel version and a matching, readable kernel/initramfs pair; file names alone do not establish that the boot artifacts are usable.
- On Arch, module cleanup only considers trees marked as restored by Bootrecov, and preserves a running kernel, installed package files, and modules referenced by remaining GRUB or BLS entries, including manual entries. Unmarked legacy trees and restored trees on other distributions remain in place.
- Cleanup retains modules if a GRUB device or configuration reference cannot be proved from the mounted filesystems; it does not execute GRUB scripts to infer their runtime state.
- Bootrecov serializes entry, snapshot, and module-tree changes across processes; Arch cleanup is deferred while a package transaction is active.
- Activation refuses a snapshot when `/usr/lib/modules/<version>` is missing and the snapshot has no matching archived module tree.
- Internal `.bootrecov` metadata is excluded from active boot mirrors.
- Reconcile removes inactive boot mirrors but preserves an already bootable active GRUB entry if refreshing its boot mirror fails transiently.
- Reconcile reports per-snapshot failures and whether an affected entry was retained; manual CLI reconcile exits nonzero on a partial failure, while package post-transaction reconcile warns without failing the completed transaction.
- There is no automatic snapshot pruning, so Bootrecov does not delete older snapshots without an explicit delete command.

## High-Risk Paths

Normal use may write to:

- `/var/backups/bootrecov-snapshots`
- `/boot/efi/bootrecov-snapshots`
- `/boot/bootrecov-snapshots` on Fedora/BLS systems
- `/usr/lib/modules/<kernel-version>` when restoring a missing archived module tree
- `/etc/grub.d/41_bootrecov_snapshots`
- `/boot/grub/grub.cfg`
- `/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook`
- `/etc/pacman.d/hooks/96-bootrecov-post-transaction.hook`
- `/usr/lib/initcpio/install/bootrecov`
- `/usr/lib/initcpio/hooks/bootrecov`
- `/etc/mkinitcpio.conf`
- `/boot/loader/entries/bootrecov-*.conf` on Fedora/BLS systems
- `/etc/dnf/libdnf5-plugins/actions.d/95-bootrecov.actions`
- `/etc/dnf/plugins/pre-transaction-actions.d/95-bootrecov.action`
- `/etc/dnf/plugins/post-transaction-actions.d/95-bootrecov.action`
- `/usr/lib/dracut/modules.d/95bootrecov`

On Ubuntu/Debian, Bootrecov can detect the platform and use the GRUB backend, but it does not install apt/dpkg hooks yet.
On Fedora-family systems, hook installation is explicit opt-in and installs scoped DNF action files plus a dracut boot-time restore module.

## Recommended Use

- Keep a rescue USB available.
- Test the full create, activate, reboot, deactivate flow in a VM or spare system first.
- Keep known-good system backups outside Bootrecov.
- Do not enable the pacman hook without monitoring snapshot disk usage.
- Keep at least one known-good kernel and matching `/usr/lib/modules/<version>` tree installed.

## Known Non-Goals

- Bootrecov does not repair a broken root filesystem.
- Bootrecov does not mount archived module SquashFS images; it extracts archived module trees only when a matching live tree is missing.
- Bootrecov does not detect failed boots automatically.
- Bootrecov does not prune snapshots automatically.
- Bootrecov does not manage systemd-boot entries yet.
- Bootrecov does not install apt/dpkg hooks yet.
