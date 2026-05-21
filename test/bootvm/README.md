# Boot VM Test (Rootless)

This directory contains a rootless QEMU-based integration test harness.

Files:

- `run_rootless_vm_test.sh`: boots a cloud VM, copies `bootrecov` into the guest, runs a smoke recovery flow, validates entry creation, and verifies reboot into the generated fallback.
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
make test-bootvm-fedora-grub-bls
make test-bootvm-grub-matrix
make test-bootvm-platform-matrix
```

The Ubuntu gate uses the Ubuntu Noble cloud image. The Debian gate uses the Debian 12 genericcloud image. The Fedora gate uses the Fedora Cloud Base image and verifies Fedora detection, the `/boot/bootrecov-snapshots` BLS mirror default, DNF/dracut hook installation, BLS entry generation, module restore, and reboot into the generated fallback. Non-default scenarios store artifacts under scenario-specific work directories such as `test/bootvm/work-debian-grub/` and `test/bootvm/work-fedora-grub-bls/`.

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

The VM guest installs Bootrecov runtime tools such as `rclone`, `squashfs-tools`, GRUB tools, and the platform initramfs/hook tools during the test.

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
- supported Fedora DNF/dracut hook installation creates DNF action files and the dracut module
- GRUB custom script dump before and after backup entry generation
- real `bootrecov backup create` generation of a compressed root module SquashFS image
- activation of the real snapshot while verifying `.bootrecov` metadata is excluded from the active boot mirror
- active fallback module restoration through the package-manager post-transaction hook path
- previous-kernel activation with archived module restoration when `/usr/lib/modules/<version>` is absent
- one-shot reboot into generated backup GRUB entry, verified by `/proc/cmdline` marker or Fedora/BLS `BOOT_IMAGE`
- corruption of primary kernel, then a second successful backup-entry reboot
