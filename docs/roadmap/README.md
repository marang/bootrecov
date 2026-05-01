# Bootrecov Roadmap

This directory is the primary product roadmap for expanding Bootrecov beyond the current Arch/GRUB-first implementation.

Bootrecov remains Linux-only and safety-first. A platform or bootloader is not considered supported until it has detection, explicit safety behavior, documentation, and tests that cover the relevant boot mutation path.

## Current Support Baseline

| Area | Current status |
| --- | --- |
| Arch Linux + GRUB + EFI | Supported |
| Arch-based distributions + GRUB + EFI | Expected to work when paths and pacman hooks match Arch conventions |
| Ubuntu/Debian + GRUB + EFI | Dedicated VM gate available; apt/dpkg hooks not implemented |
| systemd-boot | Detected and rejected for mutating operations |
| rEFInd, Limine, UKI-only, Syslinux/extlinux, U-Boot | Not supported |

## Support Levels

| Level | Meaning |
| --- | --- |
| Detected | `bootrecov doctor` can identify the platform or bootloader, but mutating commands may be blocked. |
| Experimental | Mutating commands exist and have unit coverage, but VM coverage and field confidence are incomplete. |
| Supported | Unit tests, VM/E2E coverage, README/SAFETY/AGENTS docs, and release gates are in place. |
| Package hook supported | A package-manager hook exists, has safety review, and is tested for the target package manager. |

## Delivery Order

1. Run and harden the Ubuntu/Debian GRUB VM gates until they are part of every release.
2. Design apt/dpkg hooks separately before enabling them.
3. Implement systemd-boot entry management only after backend tests are ready.
4. Add Fedora/RHEL-family detection and GRUB/BLS research.
5. Add rEFInd and Limine design documents before mutating those bootloaders.
6. Treat UKI-only / EFI stub support as a separate architecture track.

## Roadmap Documents

- [Distribution support](distro-support.md)
- [Bootloader support](bootloader-support.md)
- [Testing roadmap](testing-roadmap.md)
- [Release gates](release-gates.md)
