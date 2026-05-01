# Bootloader Support Roadmap

## Current State

GRUB is the only supported mutating backend. systemd-boot is detected but rejected for operations that would change bootloader state. Ambiguous GRUB and systemd-boot signals must remain blocked instead of guessing.

## Priority 1: GRUB Backend Hardening

Goal:

- keep GRUB safe while other adapters are added.

Implementation direction:

- Keep GRUB entry parsing, rendering, recovery commands, and `grub-mkconfig` isolated from generic snapshot logic.
- Add tests for alternative GRUB output paths under detected boot directories.
- Keep ambiguous bootloader signals unsupported.
- Keep every mutation behind risk acknowledgement.

Exit criteria:

- Existing Arch GRUB VM gate stays green.
- Ubuntu/Debian GRUB VM gates pass before declaring those distros fully supported.

## Priority 2: systemd-boot

Goal:

- add managed systemd-boot entries without reusing GRUB assumptions.

Implementation direction:

- Render entries under the detected ESP loader entries directory.
- Keep kernel, initramfs, microcode, and command-line handling backend-specific.
- Add explicit cleanup for Bootrecov-owned entries only.
- Keep detection-only behavior until unit tests and VM tests are in place.

Exit criteria:

- `bootrecov bootloader list|activate|deactivate|recovery` works through a systemd-boot adapter.
- VM test boots through a Bootrecov systemd-boot entry.
- Ambiguous GRUB + systemd-boot systems remain blocked unless the user explicitly overrides the backend.

## Later Bootloaders

| Bootloader | Roadmap status |
| --- | --- |
| rEFInd | Start with manual recovery command generation, then managed entries after design review. |
| Limine | Design first; do not mutate Limine configuration without dedicated tests. |
| UKI-only / EFI stub | Separate architecture track because kernel/initrd assumptions change. |
| Syslinux / extlinux | Not planned until user demand and test fixtures exist. |
| U-Boot | Not planned until ARM board-specific test strategy exists. |
