# Testing Roadmap

## Current Baseline

The current release gate is:

- `make test`
- `make test-bootvm-arch-grub-cleanup`
- `make test-bootvm`
- `make test-bootvm-platform-matrix`

The default rootless VM gate runs the `ubuntu-grub` scenario. `make test-bootvm-grub-matrix` runs explicit Ubuntu+GRUB and Debian+GRUB gates. `make test-bootvm-fedora-grub-bls` runs the Fedora-family GRUB/BLS gate, and `make test-bootvm-platform-matrix` runs the Ubuntu, Debian, and Fedora GRUB platform gates. The GRUB VM gates validate platform and bootloader detection, package-hook refusal where unsupported, SquashFS module archives, active boot mirror behavior, activation refusal when root modules are missing without an archive, archived module restoration when an archive exists, and reboot through a generated Bootrecov entry. The Fedora gate also verifies DNF action hook installation, dracut restore module installation, BLS entry generation, and the Fedora `/boot/bootrecov-snapshots` mirror default.

## Next VM Gates

| Gate | Purpose | Required before |
| --- | --- | --- |
| Arch + GRUB + EFI | Recovery boot and restored module/DKMS cleanup with real kernel packages | available as `make test-bootvm-arch-grub-cleanup`; every release |
| Ubuntu/Debian + GRUB + EFI | Prove non-Arch GRUB support | available as `make test-bootvm-grub-matrix`; mandatory before declaring Ubuntu/Debian fully supported |
| Fedora-family + GRUB/BLS | Prove dracut/BLS compatibility | available as `make test-bootvm-fedora-grub-bls`; mandatory before promoting Fedora-family hook or BLS changes |
| systemd-boot + EFI | Prove managed systemd-boot entries | enabling systemd-boot mutations |

## Required Scenario Shape

Every mutating bootloader backend should test:

- `doctor` detection output
- snapshot creation
- active boot mirror activation
- bootloader entry creation
- reboot through the recovery entry
- deactivation and cleanup
- refusal when required root module trees are missing
- rejection of ambiguous or unsupported bootloader signals

## Negative Coverage

Keep these regression tests permanent:

- unmarked FAT mounts are not accepted as ESP roots
- ambiguous GRUB and systemd-boot signals are rejected
- unsupported package-manager hooks fail clearly
- risk acknowledgement blocks non-interactive commands unless explicitly accepted
- internal `.bootrecov` metadata never reaches active boot mirrors
