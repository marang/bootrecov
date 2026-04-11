# bootrecov

`bootrecov` is a Go TUI for managing recovery snapshots of `/boot` and wiring selected snapshots into GRUB as bootable fallback entries.

The current design keeps two separate copies:

- snapshot source: `/var/backups/bootrecov-snapshots/<name>`
- optional EFI mirror for active GRUB entries: `/boot/efi/bootrecov-snapshots/<name>`

Inactive snapshots stay in the snapshot store only. Activating a snapshot copies it to EFI and creates a matching entry in `/etc/grub.d/41_bootrecov_snapshots`.

## What It Does

- Creates timestamped snapshots of `/boot`
- Detects kernel, initramfs, microcode images, size, and backup time
- Shows snapshots and GRUB entries in a Bubble Tea interface
- Activates or deactivates snapshots for EFI + GRUB boot recovery
- Installs a pacman pre-transaction hook that can trigger `bootrecov backup-now`
- Reconciles active EFI mirrors against stored snapshots
- Removes inactive EFI mirrors during reconcile
- Preserves an already-bootable GRUB recovery entry if a refresh fails transiently
- Regenerates `grub.cfg` after GRUB entry changes
- Prints GRUB recovery commands for an activated snapshot
- Deletes snapshots and their matching EFI/GRUB artifacts

## Current Scope

Implemented today:

- Interactive TUI only; no dedicated CLI subcommands yet
- GRUB custom entry management through `/etc/grub.d/41_bootrecov_snapshots`
- Snapshot copy profiles: `full` and `minimal`
- Non-interactive helper commands for snapshot creation, hook install, and recovery command output
- Rootless QEMU integration test harness under `test/bootvm/`
- GitHub release and AUR publish workflows for tagged releases

Not implemented today:

- Chroot / repair workflow inside a broken system
- Automatic pruning of old snapshots

## Requirements

Runtime requirements:

- Linux
- GRUB
- EFI system layout
- `rclone`
- `grub-mkconfig`

Build requirements:

- Go `1.24+`

Notes:

- The TUI operates on real system paths under `/boot`, `/boot/efi`, `/var/backups`, and `/etc/grub.d`, so normal use typically requires root privileges.
- `bootrecov` updates the custom GRUB script and then runs `grub-mkconfig -o /boot/grub/grub.cfg` by default.
- If your distro uses a different GRUB config path, adjust the code or environment before relying on the automatic regeneration path.
- The TUI performs a startup dependency check and exits immediately if required tools such as `rclone` or `grub-mkconfig` are missing.

## Install And Run

Build locally:

```bash
git clone https://github.com/marang/bootrecov.git
cd bootrecov
make build
```

Run from source:

```bash
make run
```

Or run the built binary:

```bash
./bin/bootrecov
```

The entry point is [`cmd/bootrecov/main.go`](cmd/bootrecov/main.go).

Helper commands:

```bash
bootrecov backup-now
bootrecov install-pacman-hook
bootrecov recovery-commands <snapshot-name>
```

## TUI Controls

On the backup list:

- `b`: create a new snapshot of `/boot`
- `g`: toggle EFI + GRUB activation for the selected snapshot
- `s`: reconcile EFI mirrors and clean stale GRUB state
- `r`: print GRUB recovery commands for the selected activated snapshot
- `p`: install or refresh the pacman hook
- `d`: delete the selected snapshot after confirmation
- `tab`: switch between snapshots and GRUB entries
- `q`: quit

On the GRUB entries view:

- `x`: remove the selected GRUB entry
- `tab`: return to snapshots

## Snapshot Layout

Snapshot source copies live under:

- `/var/backups/bootrecov-snapshots`

EFI mirrors for active entries live under:

- `/boot/efi/bootrecov-snapshots`

This avoids recursively copying `/boot` into `/boot/efi`. The snapshot copy excludes:

- `efi/bootrecov-snapshots/**`
- `efi/boot-backups/**`

## Activation And Reconcile Model

Activation does two things:

1. Copy the selected snapshot into the EFI mirror directory.
2. Append a matching `menuentry` to `/etc/grub.d/41_bootrecov_snapshots`.
3. Regenerate `/boot/grub/grub.cfg`.

Deactivation does the reverse:

1. Remove the matching GRUB entry.
2. Remove the EFI mirror.
3. Regenerate `/boot/grub/grub.cfg`.

Reconcile (`s`) is intentionally conservative:

- active snapshots are refreshed from the snapshot store into EFI
- inactive EFI mirrors are removed
- stale GRUB entries are removed
- if an active snapshot already has a bootable EFI mirror and refresh fails, the existing GRUB entry is kept instead of being deleted

## Pacman Hook

`bootrecov install-pacman-hook` writes:

- `/etc/pacman.d/hooks/95-bootrecov-pre-transaction.hook`

The hook runs before transactions affecting boot-critical packages and executes:

```bash
bootrecov backup-now
```

Current hook targets include:

- `linux*`
- `grub`
- `mkinitcpio`
- `systemd`

## Backup Profiles

The backup profile is selected with `BOOTRECOV_BACKUP_PROFILE`.

Default:

```bash
BOOTRECOV_BACKUP_PROFILE=full
```

Optional minimal profile:

```bash
BOOTRECOV_BACKUP_PROFILE=minimal
```

`minimal` currently includes:

- `vmlinuz*`
- `initrd.img*`
- `initramfs*.img`
- `intel-ucode.img`
- `amd-ucode.img`
- `grub/**`

The environment variable is read in [`cmd/bootrecov/main.go`](cmd/bootrecov/main.go).

## Development

Useful targets from [`Makefile`](Makefile):

- `make build` builds `bin/bootrecov`
- `make run` runs the TUI
- `make fmt` runs `gofmt`
- `make test` runs `go vet`, unit tests, race tests, and coverage tests
- `make clean` removes `bin/`

The main implementation lives in:

- [`internal/tui/backups.go`](internal/tui/backups.go)
- [`internal/tui/model.go`](internal/tui/model.go)

## Tests

Fast local checks:

```bash
go test ./...
go vet ./...
```

Broader local validation:

```bash
make test
```

CI currently runs only:

```bash
go test ./...
```

See [`.github/workflows/go-tests.yml`](.github/workflows/go-tests.yml).

## Rootless VM Integration Test

The rootless QEMU harness lives in [`test/bootvm/`](test/bootvm/).

Run the preflight check:

```bash
make test-bootvm-requirements
```

Run the integration test:

```bash
make test-bootvm
```

Optional explicit prepare step:

```bash
make test-bootvm-prepare
```

Watch mode:

```bash
make test-bootvm-watch
```

Host tools required by the harness:

- `qemu-system-x86_64`
- `qemu-img`
- `OVMF` / `edk2-ovmf`
- `ssh`
- `scp`
- `ssh-keygen`
- `curl`
- one of `cloud-localds` or `genisoimage`

Arch example:

```bash
sudo pacman -S --needed qemu-base edk2-ovmf openssh curl cloud-image-utils rclone
```

Runtime artifacts are written under `test/bootvm/work/`, including:

- `run.log`
- `status`
- `last_error`
- `serial.log`
- `ssh_port`

There is also a legacy privileged/container-based harness:

- [`test/bootvm/boot_test_vm.sh`](test/bootvm/boot_test_vm.sh)
- [`docker-compose.yml`](docker-compose.yml)

That path is retained for reference; the maintained test flow is the rootless QEMU harness.

## Packaging And Releases

GoReleaser config:

- [`.goreleaser.yml`](.goreleaser.yml)

Tagged release workflow:

- [`.github/workflows/release.yml`](.github/workflows/release.yml)

Current release artifacts are built for:

- Linux `amd64`, `arm64`

AUR packaging files:

- [`PKGBUILD`](PKGBUILD)
- [`.github/workflows/aur.yml`](.github/workflows/aur.yml)

Required GitHub secret for AUR publishing:

- `AUR_PRIVATE_KEY`

Optional AUR publish identity overrides:

- `AUR_COMMIT_NAME`
- `AUR_COMMIT_EMAIL`

## Caveats

- GRUB entry generation assumes a GRUB-based system and an EFI-visible backup path.
- Reconcile and activation depend on `rclone` unless the binary is intentionally reconfigured in code.
- The TUI is built for direct system management; it is not a dry-run planner yet.
