# Testing Roadmap

## Current Baseline

The current release gate is:

- `make test`
- `make test-bootvm`

The rootless VM gate validates the current GRUB boot path, SquashFS module archives, EFI mirror behavior, activation refusal for missing root modules, and reboot through a generated Bootrecov entry.

## Next VM Gates

| Gate | Purpose | Required before |
| --- | --- | --- |
| Arch + GRUB + EFI | Preserve current supported baseline | every release |
| Ubuntu/Debian + GRUB + EFI | Prove non-Arch GRUB support | declaring Ubuntu/Debian fully supported |
| systemd-boot + EFI | Prove managed systemd-boot entries | enabling systemd-boot mutations |
| Fedora-family + GRUB/BLS | Prove dracut/BLS compatibility | declaring Fedora-family support |

## Required Scenario Shape

Every mutating bootloader backend should test:

- `doctor` detection output
- snapshot creation
- EFI mirror activation
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
- internal `.bootrecov` metadata never reaches EFI mirrors
