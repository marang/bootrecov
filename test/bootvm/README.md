# Boot VM Test (Rootless)

This directory contains a rootless QEMU-based integration test harness.

Files:

- `run_rootless_vm_test.sh`: boots a cloud VM, copies `bootrecov` into guest, runs a smoke recovery flow, validates GRUB entry creation.
- `watch_tmux.sh`: opens a tmux dashboard with the test run and serial log tail.
- `boot_test_vm.sh`: legacy privileged/container-based harness (kept for reference).

Run from repository root:

```bash
make test-bootvm
```

`test-bootvm` runs the default `ubuntu-grub` scenario and auto-prepares cached assets on first run.

Explicit GRUB platform gates:

```bash
make test-bootvm-ubuntu-grub
make test-bootvm-debian-grub
make test-bootvm-grub-matrix
```

The Ubuntu gate uses the Ubuntu Noble cloud image. The Debian gate uses the Debian 12 genericcloud image. Non-default scenarios store artifacts under scenario-specific work directories such as `test/bootvm/work-debian-grub/`.

Optional explicit prepare step:

```bash
make test-bootvm-prepare
```

Watch mode:

```bash
make test-bootvm-watch
```

Watch mode opens three panes: the test runner, a status/log dashboard, and an interactive serial console into the guest that reconnects after reboots. Press `Ctrl-]` in the serial pane to detach from the console connection. Set `BOOTVM_SCENARIO=debian-grub` to watch the Debian gate.

Host prerequisites:

- `qemu-system-x86_64`
- `qemu-img`
- `OVMF` / `edk2-ovmf`
- `ssh`, `scp`, `ssh-keygen`
- `curl`
- `socat`
- one of `cloud-localds` or `genisoimage`

Arch install:

```bash
sudo pacman -S --needed qemu-base edk2-ovmf openssh curl cloud-image-utils socat
```

The VM guest installs Bootrecov runtime tools such as `rclone`, `squashfs-tools`, and `grub-common` during the test.

Preflight check:

```bash
make test-bootvm-requirements
```

Runtime logs (written under project directory):

- `test/bootvm/work/run.log` (full runner output)
- `test/bootvm/work/status` (current phase)
- `test/bootvm/work/last_error` (last failure summary, if any)
- `test/bootvm/work/serial.log` (VM serial console output)
- `test/bootvm/work/ssh_port` (selected local forwarded SSH port)

The test assertions include:

- `bootrecov doctor` reports the expected platform and GRUB backend
- unsupported apt/dpkg package hook installation is rejected without creating a pacman hook
- GRUB custom script dump before and after backup entry generation
- real `bootrecov backup create` generation of a compressed root module SquashFS image
- activation of the real snapshot while verifying `.bootrecov` metadata is excluded from the EFI mirror
- previous-kernel missing-module activation refusal when an archived `.sqfs` exists but `/usr/lib/modules/<version>` is absent
- one-shot reboot into generated backup GRUB entry, verified by `/proc/cmdline` marker
- corruption of primary kernel, then a second successful backup-entry reboot
