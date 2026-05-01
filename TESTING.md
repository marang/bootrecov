# Testing

Bootrecov touches boot-critical paths, so the test suite is split into fast code checks and a rootless VM end-to-end test that exercises real GRUB boot behavior.

## Fast Checks

Run:

```bash
make test
```

This executes:

- `go vet ./...`
- `go test ./...`
- `go test -race ./...`
- `go test -cover ./...`

The unit tests cover snapshot discovery, GRUB entry generation and parsing, activation safety checks, invalid snapshot names, EFI mount verification, rclone flag detection, module archive behavior, and regression cases such as closing the custom GRUB file before `grub-mkconfig`.

## Rootless VM Test

Run:

```bash
make test-bootvm
```

Watch interactively:

```bash
make test-bootvm-watch
```

The VM harness uses QEMU, OVMF, cloud-init, and an Ubuntu cloud image to validate the boot path without requiring a privileged container or host reboot.

The VM test verifies:

- host dependency preflight
- snapshot creation
- compressed SquashFS archive creation for `/usr/lib/modules/<kernel-version>`
- EFI mirror creation
- exclusion of internal `.bootrecov` metadata from EFI
- activation refusal for an old-kernel snapshot when `/usr/lib/modules/<version>` is missing
- GRUB entry generation and `grub.cfg` regeneration
- booting through the Bootrecov GRUB entry
- booting through the backup entry after corrupting the primary kernel

Watch mode creates a `bootrecov-bootvm` tmux session:

- pane 0: test runner
- pane 1: status, runner activity, run log, serial log tail
- pane 2: interactive QEMU serial console

Attach manually:

```bash
tmux attach -t bootrecov-bootvm
```

## Current Validation Baseline

Before a release, run at minimum:

```bash
make test
make test-bootvm
```

For interactive inspection, prefer:

```bash
make test-bootvm-watch
```

The VM test is the strongest current evidence that a generated Bootrecov GRUB entry is actually bootable.
