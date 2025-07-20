#!/bin/bash
set -euo pipefail

# ensure the loop driver supports partitions. some container hosts load it with
# `max_part=0`, which prevents creation of loopXpY nodes. attempt to reload the
# module with a reasonable partition count if possible.
if [[ -f /sys/module/loop/parameters/max_part && "$(cat /sys/module/loop/parameters/max_part)" -eq 0 ]]; then
  if command -v modprobe >/dev/null 2>&1; then
    modprobe -r loop >/dev/null 2>&1 || true
    modprobe loop max_part=8 >/dev/null 2>&1 || true
  fi
fi

# ensure loop device nodes exist. some container runtimes don't provide them
if [[ ! -e /dev/loop-control ]]; then
  modprobe loop 2>/dev/null || true
fi
if [[ ! -e /dev/loop-control ]]; then
  mknod /dev/loop-control c 10 237
fi
for i in $(seq 0 31); do
  [[ -e /dev/loop${i} ]] || mknod /dev/loop${i} b 7 ${i}
done

# fail early if loop devices remain inaccessible (common with rootless Podman)
if ! losetup -f >/dev/null 2>&1; then
  echo "Loop devices are required. Run this container in privileged mode as root" >&2
  echo "e.g. sudo podman compose up" >&2
  exit 1
fi

# install packages needed to build and run an Arch VM
# qemu now requires choosing a provider. "qemu-desktop" includes the SDL UI used
# in this script and replaces the old "qemu"/"qemu-arch-extra" packages.
pacman -Sy --noconfirm qemu-desktop arch-install-scripts grub efibootmgr edk2-ovmf go git parted dosfstools

# build bootrecov binary
cd /workspace/bootrecov
if [ ! -f bootrecov ]; then
    go build -o bootrecov
fi

IMG=/vm/archvm.img
MNT=/mnt/archvm

mkdir -p /vm

# create larger disk image so pacstrap doesn't run out of space
truncate -s 8G "$IMG"

# partition disk for UEFI
parted -s "$IMG" mklabel gpt
parted -s "$IMG" mkpart ESP fat32 1MiB 512MiB
parted -s "$IMG" set 1 esp on
parted -s "$IMG" mkpart primary ext4 512MiB 100%


# set up a loop device for the whole disk image
device=$(losetup --find --show "$IMG")

# obtain partition offsets and sizes from the image so we can create
# individual loop devices even when the loop driver lacks partition support
read BOOT_START BOOT_SIZE ROOT_START ROOT_SIZE < <(
  parted -sm "$IMG" unit B print |
    awk -F: '
      /^1:/ {sub(/B$/,"",$2); sub(/B$/,"",$4); bs=$2; bsz=$4}
      /^2:/ {sub(/B$/,"",$2); sub(/B$/,"",$4); rs=$2; rsz=$4}
      END {print bs, bsz, rs, rsz}'
)

boot_loop=$(losetup --find --show --offset "$BOOT_START" --sizelimit "$BOOT_SIZE" "$IMG")
root_loop=$(losetup --find --show --offset "$ROOT_START" --sizelimit "$ROOT_SIZE" "$IMG")

cleanup_loop() {
  losetup -d "$boot_loop" >/dev/null 2>&1 || true
  losetup -d "$root_loop" >/dev/null 2>&1 || true
  losetup -d "$device" >/dev/null 2>&1 || true
}

trap cleanup_loop EXIT

mkfs.fat -F32 "$boot_loop"
mkfs.ext4 "$root_loop"

mkdir -p "$MNT"
mount "$root_loop" "$MNT"
mkdir -p "$MNT/boot"
mount "$boot_loop" "$MNT/boot"

# install minimal Arch system
pacstrap "$MNT" base linux linux-firmware grub efibootmgr sudo

# copy bootrecov binary into VM
install -Dm755 ./bootrecov "$MNT/usr/local/bin/bootrecov"

# install grub
arch-chroot "$MNT" grub-install --target=x86_64-efi --efi-directory=/boot --bootloader-id=GRUB
arch-chroot "$MNT" grub-mkconfig -o /boot/grub/grub.cfg


# bootrecov requires a TTY for its interactive UI which isn't available during
# automated image creation. Skip running it here; users can invoke it manually
# once the VM boots.

umount "$MNT/boot"
umount "$MNT"
cleanup_loop
device=""

# start the VM. Use ctrl-a x to exit QEMU if using -nographic
# locate an OVMF firmware image. different distros install it to different
# paths, so check a few common locations. try installing the firmware if it's
# missing.
find_ovmf() {
  local candidates=(
    /usr/share/edk2/x64/OVMF_CODE.4m.fd
    /usr/share/edk2/x64/OVMF_CODE.fd
    /usr/share/OVMF/OVMF_CODE.fd
    /usr/share/ovmf/OVMF_CODE.fd
    /usr/share/qemu/OVMF_CODE.fd
  )
  for f in "${candidates[@]}"; do
    if [[ -f "$f" ]]; then
      echo "$f"
      return 0
    fi
  done
  # last resort: search the entire /usr/share tree
  find /usr/share -type f -iname 'OVMF_CODE*.fd' -print -quit 2>/dev/null
}

OVMF_BIOS="$(find_ovmf || true)"

if [[ -z "$OVMF_BIOS" ]]; then
  echo "OVMF firmware not found. Install the 'edk2-ovmf' or 'ovmf' package." >&2
  exit 1
fi

echo "Using OVMF firmware at $OVMF_BIOS"

qemu-system-x86_64 \
  -m 1024 \
  -drive file="$IMG",format=raw,if=virtio \
  -drive if=pflash,format=raw,unit=0,readonly=on,file="$OVMF_BIOS" \
  -display sdl

