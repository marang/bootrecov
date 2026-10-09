#!/usr/bin/env bash
# Destructive integration test: run ONLY inside the disposable Arch QEMU guest.
set -euo pipefail
export LC_ALL=C NO_COLOR=1
fail() { echo "bootrecov Arch mkinitcpio: $*" >&2; exit 1; }
[[ $EUID == 0 ]] || fail 'root is required inside the guest'
[[ -r /sys/class/dmi/id/product_name ]] || fail 'VM DMI information is missing'
case "$(cat /sys/class/dmi/id/product_name)" in
  *QEMU*|*'Standard PC'*|*KVM*) ;;
  *) fail 'requires a disposable QEMU/KVM guest' ;;
esac
[[ $(systemd-detect-virt --vm 2>/dev/null || true) == qemu || $(systemd-detect-virt --vm 2>/dev/null || true) == kvm ]] || fail 'QEMU/KVM virtualization is not detected'
[[ $(cat /var/lib/cloud/data/instance-id 2>/dev/null || true) == bootrecov-bootvm ]] || fail 'requires the Bootrecov disposable cloud-init instance'
[[ -x /tmp/bootrecov && -f /var/lib/bootrecov-vm-test/verified ]] || fail 'Arch cleanup verification must pass first'
# shellcheck disable=SC1091
source /etc/os-release
[[ ${ID:-} == arch ]] || fail 'requires Arch Linux'
# shellcheck disable=SC1091
source /var/lib/bootrecov-vm-test/versions

work=/var/lib/bootrecov-vm-test/mkinitcpio
mkdir -p "$work/presets" "$work/initcpio/install" "$work/initcpio/hooks"
export BOOTRECOV_ACCEPT_RISK=1 BOOTRECOV_PLATFORM=arch BOOTRECOV_BOOTLOADER=grub
export BOOTRECOV_BOOT_DIR=/boot BOOTRECOV_ESP_DIR="$ESP" BOOTRECOV_EFI_MIRROR_DIR=/bootrecov-snapshots
export BOOTRECOV_MKINITCPIO_CONF="$work/busybox.conf"
export BOOTRECOV_MKINITCPIO_INSTALL_HOOK="$work/initcpio/install/bootrecov"
export BOOTRECOV_MKINITCPIO_RUNTIME_HOOK="$work/initcpio/hooks/bootrecov"
export BOOTRECOV_PACMAN_HOOK_PATH="$work/bootrecov-pre.hook"
export BOOTRECOV_PACMAN_POST_HOOK_PATH="$work/bootrecov-post.hook"
export MKINITCPIO_PRESETS="$work/presets"
export MKINITCPIO_INSTALL="$work/initcpio/install:/usr/lib/initcpio/install"
export MKINITCPIO_HOOKS="$work/initcpio/hooks:/usr/lib/initcpio/hooks"
br() { /tmp/bootrecov "$@"; }

case ${1:-} in
prepare)
  printf 'HOOKS=(base systemd block filesystems)\n' >"$work/systemd.conf"
  systemd_config_before=$(sha256sum "$work/systemd.conf")
  if BOOTRECOV_MKINITCPIO_CONF="$work/systemd.conf" br hook install /tmp/bootrecov >"$work/systemd-install.log" 2>&1; then
    fail 'systemd initramfs was accepted'
  fi
  grep -Fq "systemd initramfs does not run Bootrecov's BusyBox runtime hook" "$work/systemd-install.log" || fail 'systemd refusal reason missing'
  BOOTRECOV_MKINITCPIO_CONF="$work/systemd.conf" br doctor >"$work/systemd-doctor.log"
  grep -q 'initramfs-backend.*unsupported' "$work/systemd-doctor.log" || fail 'doctor did not report unsupported initramfs'
  grep -Fq "systemd initramfs does not run Bootrecov's BusyBox runtime hook" "$work/systemd-doctor.log" || fail 'doctor did not explain systemd mode'
  for path in "$BOOTRECOV_MKINITCPIO_INSTALL_HOOK" "$BOOTRECOV_MKINITCPIO_RUNTIME_HOOK" "$BOOTRECOV_PACMAN_HOOK_PATH" "$BOOTRECOV_PACMAN_POST_HOOK_PATH"; do
    [[ ! -e $path ]] || fail "systemd refusal wrote $path"
  done
  [[ $(sha256sum "$work/systemd.conf") == "$systemd_config_before" ]] || fail 'systemd refusal changed mkinitcpio config'
  echo 'PASS real Arch guest refuses systemd mkinitcpio before writes'

  cat >"$work/busybox.conf" <<'CONFIG'
MODULES=(virtio_pci virtio_blk virtio_net ext4)
HOOKS=(base udev autodetect modconf block filesystems fsck)
CONFIG
  cat >"$work/presets/bootrecov-test.preset" <<PRESET
ALL_kver='/boot/vmlinuz-linux-lts'
ALL_config='$work/busybox.conf'
PRESETS=('default')
default_image='$work/bootrecov.img'
PRESET
  if ! br hook install /tmp/bootrecov >"$work/busybox-install.log" 2>&1; then
    tail -80 "$work/busybox-install.log" >&2
    fail 'real mkinitcpio -P build failed'
  fi
  [[ -s $work/bootrecov.img ]] || fail 'mkinitcpio did not create an image'
  lsinitcpio "$work/bootrecov.img" >"$work/image-files.log"
  grep -Eq '(^|/)hooks/bootrecov$' "$work/image-files.log" || fail 'built image lacks the Bootrecov runtime hook'
  echo 'PASS real BusyBox mkinitcpio build contains Bootrecov runtime hook'

  cp "$work/bootrecov.img" "/var/backups/bootrecov-snapshots/keep/initramfs-$B.img"
  br backup activate keep
  keep_id=$(br bootloader list | awk '$2 == "keep" {print $1}')
  [[ $keep_id =~ ^bootrecov-[a-f0-9]{12}$ ]] || fail 'cannot identify keep recovery entry'
  grub-reboot "$keep_id"
  mv "/usr/lib/modules/$B" "$work/held-modules"
  [[ ! -e /usr/lib/modules/$B ]] || fail 'modules were not removed before reboot'
  echo "ARCH_MKINITCPIO_PREPARED kernel=$B entry=$keep_id; reboot to test runtime restore"
  ;;
verify)
  [[ $(uname -r) == "$B" ]] || fail "expected fallback kernel $B, running $(uname -r)"
  grep -q 'bootrecov_entry=' /proc/cmdline || fail 'did not boot a Bootrecov recovery entry'
  target="/usr/lib/modules/$B"
  [[ -d $target && -s $target/modules.dep ]] || fail 'runtime hook did not restore the module tree'
  [[ -f $target/.bootrecov-restored && ! -L $target/.bootrecov-restored ]] || fail 'runtime hook did not mark restored modules'
  [[ $(cat "$target/.bootrecov-restored") == "$(printf '%s\n%s' "$B" "$(stat -c %i "$target")")" ]] || fail 'restored marker does not match module inode'
  echo "ARCH_MKINITCPIO_VERIFIED running=$B; real BusyBox runtime hook restored modules"
  ;;
*) fail 'usage: guest_arch_mkinitcpio.sh prepare|verify' ;;
esac
