# bootrecov

<p align="center">
  <img src="docs/assets/bootrecov-logo.png" alt="bootrecov logo" width="220">
</p>

`bootrecov` is a Linux-only CLI and TUI for creating inspectable `/boot` recovery snapshots and exposing selected snapshots as bootloader fallback entries.

It is aimed at Linux EFI systems where you want a simple recovery path for kernel, initramfs, microcode, and bootloader state without doing a full root filesystem rollback. GRUB is the currently supported bootloader backend; Arch is the fully supported hook platform, with Ubuntu/Debian detection and GRUB layout support now available.

## No Warranty / Own Risk

Bootrecov touches boot-critical files and can make a system unbootable. It is provided without warranty, and you use it entirely at your own risk.

Every TUI or CLI invocation requires an explicit acknowledgement. Interactive runs ask you to type `I UNDERSTAND`. Non-interactive automation must pass `--yes-i-understand` or set `BOOTRECOV_ACCEPT_RISK=1` for that invocation.

## What It Can Do

- Create timestamped snapshots of `/boot`.
- Store snapshots under `/var/backups/bootrecov-snapshots/<name>`.
- Optionally mirror selected snapshots into `/boot/efi/bootrecov-snapshots/<name>` for booting.
- Detect the current Linux platform and bootloader with explicit override support.
- Generate Bootrecov GRUB entries in `/etc/grub.d/41_bootrecov_snapshots`.
- Regenerate `/boot/grub/grub.cfg` after GRUB entry changes.
- Activate and deactivate recovery entries from the CLI or TUI.
- Reconcile EFI mirrors and bootloader entries against the snapshot store.
- Remove stale inactive EFI mirrors.
- Preserve an already bootable GRUB entry if refreshing its active EFI mirror fails transiently.
- Print GRUB recovery commands for an activated snapshot.
- Install a pacman pre-transaction hook to create snapshots before boot-critical package changes.
- Archive the matching `/usr/lib/modules/<kernel-version>` tree as compressed SquashFS metadata inside the snapshot source.
- Refuse activation when a snapshot clearly needs `/usr/lib/modules/<kernel-version>` but that module tree is missing on the root filesystem.
- Validate snapshot names before path-sensitive operations.
- Verify the EFI mount before activation or reconcile mutates EFI state, so it does not silently write into an unmounted `/boot/efi` directory.
- Report detected platform, bootloader, paths, and support status with `bootrecov doctor`.

## What It Does Not Do

- It does not automatically restore or overwrite `/usr/lib/modules`.
- It does not mount the archived SquashFS module image during activation.
- It does not repair a broken root filesystem.
- It does not detect failed boots automatically.
- It does not prune old snapshots automatically yet.
- It does not install apt/dpkg hooks yet.
- It detects `systemd-boot`, but does not manage systemd-boot entries yet.
- It is not a replacement for a rescue USB or real system backups.

The archived module SquashFS is there to make the backup complete and inspectable. Activation stays conservative: if you want to boot an older kernel, the matching `/usr/lib/modules/<version>` must already exist on the root filesystem.

## Storage Model

Bootrecov keeps two locations with different purposes:

- Snapshot source: `/var/backups/bootrecov-snapshots/<name>`
- EFI mirror: `/boot/efi/bootrecov-snapshots/<name>`

New snapshots are written only to the snapshot source. EFI mirrors are created only when a snapshot is activated for GRUB booting.

Module archives are stored only in the snapshot source:

```text
/var/backups/bootrecov-snapshots/<name>/.bootrecov/root-modules/<kernel-version>.sqfs
```

The internal `.bootrecov` metadata is intentionally excluded from EFI mirrors so the EFI system partition does not get filled with root filesystem module trees.

## Requirements

Runtime:

- Linux
- GRUB for boot entry management
- EFI system partition mounted at the expected location
- `rclone`
- `grub-mkconfig`
- `mksquashfs` from `squashfs-tools`

Build:

- Go `1.25+`

Normal operation usually requires root because Bootrecov writes to:

- `/var/backups/bootrecov-snapshots`
- `/boot/efi/bootrecov-snapshots`
- `/etc/grub.d/41_bootrecov_snapshots`
- `/boot/grub/grub.cfg`
- `/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook`

## Support Matrix

### Distribution Support

| Distribution / platform | Status | Package hook |
| --- | --- | --- |
| Arch Linux | Supported for Linux + EFI + GRUB systems | pacman hook supported |
| Arch-based distributions | Expected to work when their `/etc/os-release` and GRUB/EFI layout match Arch conventions | pacman hook supported when pacman hook paths are present |
| Ubuntu | Supported for Linux + EFI + GRUB layouts | apt/dpkg hook planned, not implemented |
| Debian | Supported for Linux + EFI + GRUB layouts | apt/dpkg hook planned, not implemented |
| Other Linux distributions | Experimental via detection and environment overrides | not implemented |

### Bootloader Support

| Bootloader | Status |
| --- | --- |
| GRUB | Supported backend |
| systemd-boot | Detected, but not managed yet |
| rEFInd | Not supported yet |
| Limine | Not supported yet |
| UKI-only / EFI stub | Not supported yet |
| Syslinux / extlinux | Not supported yet |
| U-Boot | Not supported yet |

Runtime detection uses `/etc/os-release`, mount information from `/proc/self/mountinfo`, visible boot artifacts, and existing bootloader files. Bootrecov can detect common layouts such as `/boot/efi`, `/efi`, and ESP-at-`/boot`; explicit overrides still win for unusual systems or tests:

```bash
BOOTRECOV_PLATFORM=ubuntu
BOOTRECOV_BOOTLOADER=grub
BOOTRECOV_BOOT_DIR=/boot
BOOTRECOV_ESP_DIR=/boot/efi
BOOTRECOV_EFI_MIRROR_DIR=/boot/efi/bootrecov-snapshots
```

## Install And Build

Build locally:

```bash
git clone https://github.com/marang/bootrecov.git
cd bootrecov
make build
```

Run the TUI from source:

```bash
make run
```

Run the built binary:

```bash
sudo ./bin/bootrecov
```

For system use, install the binary somewhere stable, for example `/usr/bin/bootrecov`.

## Quick Start

Create a snapshot:

```bash
sudo bootrecov backup create
```

List snapshots:

```bash
sudo bootrecov backup list
```

Activate a snapshot as an EFI + bootloader fallback:

```bash
sudo bootrecov backup activate <snapshot-name>
```

List Bootrecov bootloader entries:

```bash
sudo bootrecov bootloader list
```

Deactivate a snapshot:

```bash
sudo bootrecov backup deactivate <snapshot-name>
```

Delete a snapshot and related artifacts:

```bash
sudo bootrecov backup delete <snapshot-name>
```

Reconcile stored snapshots, EFI mirrors, and bootloader entries:

```bash
sudo bootrecov reconcile
```

Inspect detected platform and bootloader state:

```bash
bootrecov doctor
```

## CLI Reference

Start the TUI:

```bash
bootrecov
bootrecov tui
```

Inspect runtime detection:

```bash
bootrecov doctor
```

Manage snapshots:

```bash
bootrecov backup list
bootrecov backup create
bootrecov backup activate <snapshot-name>
bootrecov backup deactivate <snapshot-name>
bootrecov backup delete <snapshot-name>
bootrecov backup recovery <snapshot-name>
```

Manage bootloader state:

```bash
bootrecov bootloader list
bootrecov bootloader activate <snapshot-name>
bootrecov bootloader deactivate <snapshot-name>
bootrecov bootloader recovery <snapshot-name>
bootrecov reconcile
```

`bootrecov grub list` is retained as a deprecated compatibility alias for `bootrecov bootloader list`.

Install the package-manager hook:

```bash
bootrecov hook install
bootrecov hook install /absolute/path/to/bootrecov
```

Hook installation is currently implemented for Arch/pacman. Ubuntu/Debian apt/dpkg hook support is planned but intentionally not enabled yet.

Compatibility aliases retained for existing automation:

```bash
bootrecov backup-now
bootrecov install-pacman-hook
bootrecov recovery-commands <snapshot-name>
```

`backup list` shows:

- `SNAPSHOT`: snapshot exists in `/var/backups/bootrecov-snapshots`
- `EFI`: active EFI mirror exists
- `BOOTLOADER`: Bootrecov bootloader entry exists
- `BOOTABLE`: snapshot is complete, active, synced, and not missing known required modules
- `ROOT-MODULES`: `yes`, `missing`, `archived`, or `unknown`
- `KERNEL`: detected kernel version

## TUI Controls

Backups view:

- `b`: create snapshot
- `g`: toggle EFI + bootloader activation
- `s`: reconcile EFI mirrors and bootloader state
- `r`: show recovery commands for selected backup
- `p`: install package-manager hook
- `d`: delete selected backup, with confirmation
- `tab`: switch to bootloader entries
- `q`: quit

Bootloader entries view:

- `x`: remove selected bootloader entry
- `tab`: switch back to backups
- `q`: quit

## Backup Profiles

The backup profile is selected with `BOOTRECOV_BACKUP_PROFILE`.

Default full profile:

```bash
BOOTRECOV_BACKUP_PROFILE=full
```

Minimal profile:

```bash
BOOTRECOV_BACKUP_PROFILE=minimal
```

`full` copies the `/boot` tree while excluding the mounted ESP subtree such as `/boot/efi/**`, so firmware files are not duplicated into snapshots or active EFI mirrors.

`minimal` includes:

- `vmlinuz*`
- `initrd.img*`
- `initramfs*.img`
- `intel-ucode.img`
- `amd-ucode.img`
- `grub/**`

Both profiles can archive the matching `/usr/lib/modules/<kernel-version>` tree into `.bootrecov/root-modules/` when the kernel version is known and the module tree exists.

## Activation Model

Activation performs these steps:

1. Validate the snapshot name.
2. Verify the snapshot has a kernel and initramfs.
3. Verify matching root modules if the kernel version is known.
4. Verify that the EFI root is actually mounted.
5. Check available EFI space.
6. Copy the snapshot into `/boot/efi/bootrecov-snapshots/<name>`.
7. Add a Bootrecov bootloader entry. For the current backend, this is a GRUB menu entry.
8. Regenerate `/boot/grub/grub.cfg`.

Deactivation removes the bootloader entry, removes the EFI mirror, and regenerates the bootloader config when the backend requires it.

Reconcile is intentionally conservative:

- Active snapshots are refreshed into EFI.
- Inactive EFI mirrors are removed.
- Stale bootloader entries are removed.
- Entries for known missing root module trees are treated as not boot-ready.
- A previously bootable entry is preserved if refreshing its active EFI mirror fails transiently.

## Package Hooks

Install:

```bash
sudo bootrecov hook install
```

On Arch, the installed hook path is:

```text
/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook
```

The hook runs:

```bash
/usr/bin/env BOOTRECOV_ACCEPT_RISK=1 bootrecov backup-now
```

Current trigger targets:

- `linux*`
- `grub`
- `mkinitcpio`
- `systemd`

There is no automatic pruning yet, so watch disk usage if you enable the hook.

On Ubuntu/Debian, `bootrecov hook install` currently returns a clear "not implemented yet" error instead of installing an unsafe partial apt/dpkg hook.

## Recovery Commands

For an activated snapshot:

```bash
sudo bootrecov backup recovery <snapshot-name>
```

This prints backend-specific recovery commands. For the current GRUB backend, the output can be used manually from a GRUB prompt. The snapshot must already have an EFI mirror.

## Tests

Detailed testing notes are in [`TESTING.md`](TESTING.md).

Fast checks:

```bash
go test ./...
go vet ./...
```

Full local test target:

```bash
make test
```

`make test` runs:

- `go vet ./...`
- `go test ./...`
- `go test -race ./...`
- `go test -cover ./...`

## Rootless VM Integration Test

The maintained end-to-end test harness lives in [`test/bootvm/`](test/bootvm/).

Preflight:

```bash
make test-bootvm-requirements
```

Run once:

```bash
make test-bootvm
```

Run with a tmux watch UI:

```bash
make test-bootvm-watch
```

The watch UI creates a `bootrecov-bootvm` tmux session:

- pane 0: main test runner
- pane 1: status, runner activity, run log, serial log tail
- pane 2: interactive QEMU serial console

Attach manually:

```bash
tmux attach -t bootrecov-bootvm
```

Host tools required by the harness:

- `qemu-system-x86_64`
- `qemu-img`
- OVMF / `edk2-ovmf`
- `ssh`
- `scp`
- `ssh-keygen`
- `curl`
- `socat`
- one of `cloud-localds` or `genisoimage`

Arch example:

```bash
sudo pacman -S --needed qemu-base edk2-ovmf openssh curl cloud-image-utils socat
```

The VM test currently verifies:

- dependency preflight
- snapshot creation
- SquashFS module archive creation
- EFI mirror creation without leaking `.bootrecov` metadata
- old-kernel snapshot activation refusal when `/usr/lib/modules/<version>` is missing
- bootloader entry generation through the GRUB backend
- booting the Bootrecov GRUB entry
- booting the backup entry after corrupting the primary kernel

Runtime artifacts are written under `test/bootvm/work/`, including:

- `run.log`
- `status`
- `last_error`
- `serial.log`
- `ssh_port`

There is also a legacy privileged/container-based harness kept for reference:

- [`test/bootvm/boot_test_vm.sh`](test/bootvm/boot_test_vm.sh)
- [`docker-compose.yml`](docker-compose.yml)

## Packaging And Releases

User-visible changes are tracked in [`CHANGELOG.md`](CHANGELOG.md).

Release automation:

- [`.github/workflows/release.yml`](.github/workflows/release.yml)
- [`.goreleaser.yml`](.goreleaser.yml)

Release targets:

- Linux `amd64`
- Linux `arm64`

AUR packaging:

- [`PKGBUILD`](PKGBUILD)
- [`.github/workflows/aur.yml`](.github/workflows/aur.yml)

AUR runtime dependencies include:

- `rclone`
- `grub`
- `squashfs-tools`

Required GitHub secret for AUR publishing:

- `AUR_PRIVATE_KEY`

Optional AUR publish identity overrides:

- `AUR_COMMIT_NAME`
- `AUR_COMMIT_EMAIL`

## Development Notes

Useful targets:

- `make build`: build `bin/bootrecov`
- `make run`: run the TUI
- `make fmt`: run `gofmt`
- `make test`: run vet, tests, race tests, and coverage
- `make test-bootvm`: run the rootless VM integration test
- `make test-bootvm-watch`: run the VM test in tmux watch mode
- `make clean`: remove build artifacts

Main implementation:

- [`cmd/bootrecov/main.go`](cmd/bootrecov/main.go)
- [`internal/tui/backups.go`](internal/tui/backups.go)
- [`internal/tui/model.go`](internal/tui/model.go)

## Caveats

The safety model is documented in [`SAFETY.md`](SAFETY.md).

- Bootrecov assumes Linux + EFI, with GRUB as the currently supported bootloader backend.
- The default GRUB config output is `/boot/grub/grub.cfg`, with detection for GRUB config under the detected boot directory.
- The default EFI mirror root is `/boot/efi/bootrecov-snapshots`, with detection for mounted ESP roots such as `/boot/efi`, `/efi`, or ESP-at-`/boot`.
- Activation refuses to proceed if the EFI root is not mounted.
- systemd-boot is detected but not managed yet.
- Bootrecov is still young software touching high-risk boot paths. Test the full loop in a VM or spare system before relying on it.
