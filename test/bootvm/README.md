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

`test-bootvm` auto-prepares cached assets on first run.

Optional explicit prepare step:

```bash
make test-bootvm-prepare
```

Watch mode:

```bash
make test-bootvm-watch
```

Host prerequisites:

- `qemu-system-x86_64`
- `qemu-img`
- `OVMF` / `edk2-ovmf`
- `ssh`, `scp`, `ssh-keygen`
- `curl`
- one of `cloud-localds` or `genisoimage`

Arch install:

```bash
sudo pacman -S --needed qemu-base edk2-ovmf openssh curl cloud-image-utils
```

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

- GRUB custom script dump before and after backup entry generation
- one-shot reboot into generated backup GRUB entry, verified by `/proc/cmdline` marker
- corruption of primary kernel, then a second successful backup-entry reboot
