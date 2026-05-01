# Distribution Support Roadmap

## Current State

Bootrecov currently has platform detection for Arch, Ubuntu, and Debian through `/etc/os-release`. Arch has pacman hook support. Ubuntu and Debian can use the GRUB backend when the detected boot layout matches, but apt/dpkg hooks are intentionally not implemented yet.

## Priority 1: Ubuntu and Debian

Goal:

- make Ubuntu/Debian + GRUB + EFI a supported combination, not just detected GRUB layouts.

Implementation direction:

- Keep GRUB as the only mutating backend for this phase.
- Document supported path variants: `/boot`, `/boot/efi`, `/efi`, and ESP-at-`/boot`.
- Add a dedicated Ubuntu/Debian VM release gate that runs create, activate, reboot, deactivate, and recovery refusal checks.
- Add `doctor` warnings when apt/dpkg hook support is unavailable.
- Design apt/dpkg hooks in a separate document before implementation.

Out of scope for this phase:

- automatic apt/dpkg hook installation
- restoring or mounting `/usr/lib/modules`
- automatic fallback after failed boots

Exit criteria:

- Ubuntu/Debian GRUB scenario passes in VM.
- README and SAFETY describe the exact support status.
- `bootrecov hook install` still refuses apt/dpkg platforms until hook safety is designed.

## Priority 2: Fedora and RHEL-Family

Goal:

- add Fedora-family detection and a research-backed plan for GRUB/BLS and dracut-based systems.

Implementation direction:

- Detect Fedora, CentOS Stream, RHEL-compatible distributions, and ID_LIKE variants.
- Treat dracut and BLS-style boot entries as first-class compatibility questions.
- Start with `doctor`, detection tests, and non-mutating inspection.
- Do not ship package-manager hooks in the first Fedora phase.

Exit criteria:

- Fedora-family platforms are detected accurately.
- Mutating support remains blocked or experimental until a VM scenario exists.

## Later Distributions

| Distribution family | Roadmap status |
| --- | --- |
| openSUSE | Research after Fedora/RHEL, because bootloader tooling and snapshot expectations differ. |
| Gentoo | Manual/advanced use only until explicit demand and test fixtures exist. |
| NixOS | Separate design required; generated boot configuration should not be mutated naively. |
| Alpine | Separate design required; bootloader and initramfs conventions differ substantially. |
