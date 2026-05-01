# External Agent Review

Date: 2026-05-01

I’d treat bootrecov as promising but experimental, not something I’d rely on as my only boot recovery plan yet.

## What looks good

- The AUR package is simple: builds a Go binary from `https://github.com/marang/bootrecov`, installs `/usr/bin/bootrecov`, no post-install script or hidden root-time hook.
- The source tarball is pinned with a SHA-256 checksum.
- Runtime deps are explicit: `rclone` and `grub`.
- `go test ./...` and `go vet ./...` both pass locally.
- The idea is useful: snapshot `/boot`, mirror selected snapshots into EFI, and generate GRUB fallback entries.

## What makes me cautious

- It is very new: first submitted and last updated on April 11, 2026, version `0.1.2-1`.
- AUR metadata currently shows 0 votes and 0 popularity, so there is basically no community signal yet.
- It touches high-risk paths: `/boot`, `/boot/efi`, `/etc/grub.d`, `/boot/grub/grub.cfg`, and can install a pacman pre-transaction hook.
- It assumes a fairly specific Linux + GRUB + EFI layout.
- There is no automatic pruning yet, so repeated pacman-hook snapshots could grow storage usage.
- I’d want stricter snapshot-name validation before being fully comfortable, because some code paths validate names more carefully than others.

## Practical take

Fine to test on an Arch GRUB/EFI system if you already have a known-good rescue USB and backups. I would not install it on a machine where boot reliability matters until you have tested the full loop: create snapshot, activate it, regenerate GRUB, boot it, deactivate it, and recover from a deliberately broken `/boot` in a VM or spare machine.
