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

# helper to wait for partition device nodes
wait_for_partitions() {
  local dev="$1"
  for _ in $(seq 1 50); do
    if [[ -e "${dev}p1" && -e "${dev}p2" ]]; then
      return 0
    fi
    partprobe "$dev" >/dev/null 2>&1 || true
    partx -u "$dev" >/dev/null 2>&1 || true
    if command -v udevadm >/dev/null 2>&1; then
      udevadm settle --timeout=1 --exit-if-exists="${dev}p2" >/dev/null 2>&1 || true
    fi
    sleep 0.1
  done
  return 1
}

# setup loop device and expose partitions
device=$(losetup --find --show -P "$IMG")
if ! wait_for_partitions "$device"; then
    echo "Failed to create loop partitions for $device" >&2
    exit 1
fi
mkfs.fat -F32 "${device}p1"
mkfs.ext4 "${device}p2"

mkdir -p "$MNT"
mount "${device}p2" "$MNT"
mkdir -p "$MNT/boot"
mount "${device}p1" "$MNT/boot"

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
losetup -d "$device"

# start the VM. Use ctrl-a x to exit QEMU if using -nographic
# locate an OVMF firmware image. different distros install it to different
# paths, so check a few common locations. try installing the firmware if it's
# missing.
find_ovmf() {
  for p in \
    /usr/share/edk2-ovmf/x64/OVMF_CODE.fd \
    /usr/share/edk2/ovmf/OVMF_CODE.fd \
    /usr/share/edk2/ovmf/x64/OVMF_CODE.fd \
    /usr/share/OVMF/OVMF_CODE.fd \
    /usr/share/OVMF/OVMF_CODE_4M.fd \
    /usr/share/OVMF/OVMF.fd \
    /usr/share/qemu/OVMF_CODE.fd \
    /usr/share/qemu/OVMF.fd; do
    [[ -f "$p" ]] && { echo "$p"; return 0; }
  done
  return 1
}

OVMF_BIOS="$(find_ovmf || true)"

if [[ -z "$OVMF_BIOS" ]]; then
  if command -v pacman >/dev/null 2>&1; then
    pacman -Sy --noconfirm edk2-ovmf ovmf >/dev/null 2>&1 || true
  elif command -v apt-get >/dev/null 2>&1; then
    apt-get update >/dev/null 2>&1 && apt-get install -y ovmf >/dev/null 2>&1 || true
  fi
  OVMF_BIOS="$(find_ovmf || true)"
fi

if [[ -z "$OVMF_BIOS" ]]; then
  echo "OVMF firmware not found. Install the 'edk2-ovmf' or 'ovmf' package." >&2
  exit 1
fi

qemu-system-x86_64 \
  -m 1024 \
  -drive file="$IMG",format=raw,if=virtio \
  -bios "$OVMF_BIOS" \
  -display sdl

