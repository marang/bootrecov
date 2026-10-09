# Boot VM Test (Rootless)

This directory contains a rootless QEMU-based integration test harness.

Files:

- `run_rootless_vm_test.sh`: boots a cloud VM, copies `bootrecov` into the guest, runs a smoke recovery flow, validates entry creation, and verifies reboot into the generated fallback.
- `guest_arch_cleanup.sh`: guest-only Arch setup and assertions for recovery boot, restored module ownership, shared kernels, Windows entries, unknown chainloaders, and real DKMS builds.
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
make test-bootvm-arch-grub-cleanup
make test-bootvm-debian-grub
make test-bootvm-fedora-grub-bls
make test-bootvm-grub-matrix
make test-bootvm-platform-matrix
```

The Ubuntu gate uses the Ubuntu Noble cloud image. The Debian gate uses the Debian 12 genericcloud image. The Fedora gate uses the Fedora Cloud Base image and verifies Fedora detection, a custom `/boot/custom-recovery` BLS mirror override, DNF/dracut hook installation, hook inclusion in the selected kernel initramfs, BLS entry generation, module restore, and reboot into the generated fallback. Before reboot, it removes the running kernel's module tree inside the disposable guest, then checks that the installed dracut hook restored it from the separately mounted `/var` archive despite the initially read-only root and wrote an inode-matched ownership marker. Non-default scenarios store artifacts under scenario-specific work directories such as `test/bootvm/work-debian-grub/` and `test/bootvm/work-fedora-grub-bls/`.

The separate `arch-grub-cleanup` gate uses the official Arch cloud image in a disposable 24 GiB overlay and installs real GRUB, linux/linux-lts kernels and headers, pacman dependencies, and DKMS inside the guest. It stores active mirrors at the custom `/custom-recovery` path on the guest root filesystem. After GRUB installation, the runner powers off the guest and resets its disposable OVMF variables so the cloud image's initial systemd-boot selection does not bypass GRUB. It boots through a generated Bootrecov recovery and checks both deactivation and deletion with a remaining recovery, a Windows chainloader fixture, shared kernel versions, and unknown chainloaders/BootNext selections. A second recovery boot checks that the BusyBox mkinitcpio hook restores a missing module tree using the snapshot marker with that custom mirror. The cloud image’s automatic BootNext menu generator is disabled inside the guest to isolate the supported cleanup scenario; an explicit BootNext fixture must block cleanup. The Windows fixture tests GRUB configuration handling; it does not install or boot Windows. No host boot configuration, packages, or module trees are modified. The generated `grub.cfg` is captured before verification for diagnosis. Artifacts are stored under `test/bootvm/work-arch-grub-cleanup/`; KVM is used when available, with QEMU software emulation as fallback. This gate is separate from the Ubuntu/Debian/Fedora platform matrix.

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
- Fedora recovery reboot restores the removed module tree through the installed dracut hook with a custom mirror path
- corruption of primary kernel, then a second successful backup-entry reboot
