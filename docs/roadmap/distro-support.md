# Distribution Support Roadmap

## Current State

Bootrecov currently has platform detection for Arch, Fedora-family, Ubuntu, and Debian through `/etc/os-release`. Arch has pacman/BusyBox-mkinitcpio hook support; systemd-based and ambiguous mkinitcpio configurations are refused because there is no systemd initramfs restore unit. Fedora-family systems have DNF/dracut hook support, BLS-first GRUB entry support when the active mirror is on the same boot filesystem, a Fedora `/boot/bootrecov-snapshots` mirror default for BLS layouts, and an explicit Fedora GRUB/BLS VM gate. Ubuntu and Debian have explicit GRUB + EFI VM gate targets, and apt/dpkg hooks are intentionally not implemented yet.

## Priority 1: Ubuntu and Debian

Goal:

- harden Ubuntu/Debian + GRUB + EFI until the VM gates are reliable enough to be mandatory release gates.

Implementation direction:

- Keep GRUB as the only mutating backend for this phase.
- Document supported path variants: `/boot`, `/boot/efi`, `/efi`, and ESP-at-`/boot`.
- Keep the dedicated Ubuntu/Debian VM gates running create, activate, reboot, deactivate, and recovery refusal checks.
- Add `doctor` warnings when apt/dpkg hook support is unavailable.
- Design apt/dpkg hooks in a separate document before implementation.

Out of scope for this phase:

- automatic apt/dpkg hook installation
- apt/dpkg hooks and initramfs-tools boot-time restore
- automatic fallback after failed boots

Exit criteria:

- Ubuntu/Debian GRUB scenarios pass in VM.
- README and SAFETY describe the exact support status.
- `bootrecov hook install` still refuses apt/dpkg platforms until hook safety is designed.

## Priority 2: Fedora and RHEL-Family

Goal:

- harden Fedora-family GRUB/BLS and dracut-based systems with VM coverage.

Implementation direction:

- Keep DNF5/DNF4 action files and dracut module installation behind explicit `bootrecov hook install`.
- Prefer BLS entries when they can reference the active mirror from the same boot filesystem; otherwise use the GRUB custom-entry backend. Fedora/BLS defaults active mirrors to `/boot/bootrecov-snapshots` unless the user explicitly overrides the mirror path.
- Keep Fedora VM coverage mandatory before changing Fedora bootloader, DNF hook, or dracut behavior.

Exit criteria:

- Fedora-family platforms are detected accurately.
- Fedora GRUB/BLS VM scenario boots a generated Bootrecov recovery entry.
- DNF/dracut hook installation is verified in VM; install and uninstall behavior is covered by unit tests.

## Later Distributions

| Distribution family | Roadmap status |
| --- | --- |
| openSUSE | Research after Fedora/RHEL, because bootloader tooling and snapshot expectations differ. |
| Gentoo | Manual/advanced use only until explicit demand and test fixtures exist. |
| NixOS | Separate design required; generated boot configuration should not be mutated naively. |
| Alpine | Separate design required; bootloader and initramfs conventions differ substantially. |
