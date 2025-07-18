#!/bin/bash
set -euo pipefail

# ensure loop device nodes exist. some container runtimes don't provide them
if [[ ! -e /dev/loop-control ]]; then
  modprobe loop 2>/dev/null || true
fi
if [[ ! -e /dev/loop-control ]]; then
  mknod /dev/loop-control c 10 237
fi
for i in $(seq 0 7); do
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

# create 2G disk image quickly
truncate -s 2G "$IMG"

# partition disk for UEFI
parted -s "$IMG" mklabel gpt
parted -s "$IMG" mkpart ESP fat32 1MiB 256MiB
parted -s "$IMG" set 1 esp on
parted -s "$IMG" mkpart primary ext4 256MiB 100%

# setup loop device and expose partitions
device=$(losetup --find --show -P "$IMG")
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

# run bootrecov once to generate backup and grub entry
arch-chroot "$MNT" bootrecov || true

umount "$MNT/boot"
umount "$MNT"
losetup -d "$device"

# start the VM. Use ctrl-a x to exit QEMU if using -nographic
qemu-system-x86_64 \
  -m 1024 \
  -drive file="$IMG",format=raw,if=virtio \
  -bios /usr/share/edk2-ovmf/x64/OVMF_CODE.fd \
  -display sdl
