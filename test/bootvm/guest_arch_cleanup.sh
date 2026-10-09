#!/usr/bin/env bash
# Destructive integration test: run ONLY inside the disposable Arch QEMU guest.
set -euo pipefail
export LC_ALL=C
fail() { echo "bootrecov Arch cleanup: $*" >&2; exit 1; }
[[ $EUID == 0 ]] || fail 'root is required inside the guest'
# Refuse the host even if invoked accidentally through sudo.
[[ -r /sys/class/dmi/id/product_name ]] || fail 'VM DMI information is missing'
case "$(cat /sys/class/dmi/id/product_name)" in
  *QEMU*|*'Standard PC'*|*KVM*) ;;
  *) fail 'requires a disposable QEMU/KVM guest' ;;
esac
[[ $(systemd-detect-virt --vm 2>/dev/null || true) == qemu || $(systemd-detect-virt --vm 2>/dev/null || true) == kvm ]] || fail 'QEMU/KVM virtualization is not detected'
[[ $(cat /var/lib/cloud/data/instance-id 2>/dev/null || true) == bootrecov-bootvm ]] || fail 'requires the Bootrecov disposable cloud-init instance'
# shellcheck disable=SC1091
source /etc/os-release
[[ ${ID:-} == arch ]] || fail 'requires Arch Linux'
[[ -d /sys/firmware/efi ]] || fail 'requires a UEFI guest'
[[ -x /tmp/bootrecov ]] || fail '/tmp/bootrecov is missing'
export BOOTRECOV_ACCEPT_RISK=1 BOOTRECOV_PLATFORM=arch BOOTRECOV_BOOTLOADER=grub
export BOOTRECOV_BOOT_DIR=/boot BOOTRECOV_EFI_MIRROR_DIR=/custom-recovery
export BOOTRECOV_ROOT_MODULES_DIR=/usr/lib/modules
state=/var/lib/bootrecov-vm-test
mkdir -p "$state"
trap 'rc=$?; if ((rc)); then echo "Arch cleanup phase failed at line $LINENO; logs: '$state'" >&2; fi' EXIT
br() { /tmp/bootrecov "$@"; }
log_run() {
  local log=$1; shift
  if ! "$@" >"$state/$log" 2>&1; then tail -80 "$state/$log" >&2; fail "$* failed (see $state/$log)"; fi
}
version_for() {
  local pkg=$1 f
  for f in /usr/lib/modules/*/pkgbase; do
    [[ -f $f && $(cat "$f") == "$pkg" ]] && { basename "$(dirname "$f")"; return; }
  done
  fail "cannot find module version for $pkg"
}
assert_tree() { [[ -d /usr/lib/modules/$1 ]] || fail "module tree $1 was removed"; }
assert_absent() { [[ ! -e /usr/lib/modules/$1 ]] || fail "unused module tree $1 remains"; }
assert_marked() {
  local target=/usr/lib/modules/$1
  assert_tree "$1"
  [[ -f $target/.bootrecov-restored && ! -L $target/.bootrecov-restored ]] || fail "restored marker missing for $1"
  [[ $(cat "$target/.bootrecov-restored") == "$(printf '%s\n%s' "$1" "$(stat -c %i "$target")")" ]] || fail "marker inode mismatch for $1"
}
assert_entry_absent() {
  local status=0
  grep -Fq -- "menuentry 'Bootrecov $BOOTRECOV_EFI_MIRROR_DIR/$1'" /boot/grub/grub.cfg || status=$?
  [[ $status == 1 ]] || fail "entry $1 remains or GRUB config is unreadable"
}
entry_id() {
  local entries id
  entries=$(br bootloader list) || fail 'cannot query bootloader entries'
  id=$(awk -v name="$1" '$2 == name {print $1}' <<<"$entries")
  [[ $id =~ ^bootrecov-[a-f0-9]{12}$ ]] || fail "cannot identify recovery $1"
  printf '%s\n' "$id"
}
assert_keep() {
  assert_marked "$B"
  grep -Fq -- "menuentry 'Bootrecov $BOOTRECOV_EFI_MIRROR_DIR/keep'" /boot/grub/grub.cfg || fail 'remaining recovery entry disappeared'
  grep -q 'chainloader /EFI/Microsoft/Boot/bootmgfw.efi' /boot/grub/grub.cfg || fail 'Windows fixture disappeared'
  if [[ -f $state/prepared ]]; then
    dkms status -m bootrecov_probe -v 1.0 | grep -F "$B" >/dev/null || fail "DKMS build for remaining kernel $B disappeared"
  fi
}
assert_dkms() {
  dkms status -m bootrecov_probe -v 1.0 | grep -F "$A" >/dev/null || fail "DKMS build for $A is missing"
}
assert_no_dkms() {
  local status
  status=$(dkms status -m bootrecov_probe -v 1.0) || fail 'cannot query DKMS status'
  if grep -F "$A" <<<"$status" >/dev/null; then fail "DKMS build for $A remains"; fi
}
snapshot() {
  local name=$1 version=$2 pkg=$3 target=/var/backups/bootrecov-snapshots/$1
  mkdir -p "$target/.bootrecov/root-modules"
  cp "/boot/vmlinuz-$pkg" "$target/vmlinuz-$version"
  cp "/boot/initramfs-$pkg.img" "$target/initramfs-$version.img"
  # Headers are irrelevant to restoration; preserving them separately enables
  # a genuine DKMS build after linux-headers has been removed by pacman.
  log_run "archive-$name.log" mksquashfs "/usr/lib/modules/$version" "$target/.bootrecov/root-modules/$version.sqfs" -noappend -processors 2 -e build source
}
build_probe() {
  ln -sfn /opt/bootrecov-A-headers "/usr/lib/modules/$A/build"
  log_run dkms-install-A.log dkms install -m bootrecov_probe -v 1.0 -k "$A"
  assert_dkms
}
restore_old() { br backup activate old; assert_marked "$A"; build_probe; }
case ${1:-} in
prepare)
  [[ ! -e $state/prepared ]] || fail 'prepare already ran; start with a fresh disposable guest'
  log_run packages.log pacman -Syu --noconfirm --needed linux linux-headers linux-lts linux-lts-headers grub mkinitcpio dkms base-devel rclone squashfs-tools file
  A=$(version_for linux); B=$(version_for linux-lts)
  [[ $A != "$B" && $(uname -r) != "$B" ]] || fail 'prepare requires two different kernels and must not run linux-lts'
  printf 'A=%q\nB=%q\n' "$A" "$B" >"$state/versions"
  cp -a "/usr/lib/modules/$A/build" /opt/bootrecov-A-headers
  # Ensure initramfs includes the emulated storage/network drivers independent
  # of the cloud image's previous initramfs backend.
  log_run mkinitcpio-A.log mkinitcpio -k "$A" -g /boot/initramfs-linux.img
  log_run mkinitcpio-B.log mkinitcpio -k "$B" -g /boot/initramfs-linux-lts.img
  esp=''
  for candidate in /boot /boot/efi /efi; do
    # Access systemd automounts first and exclude their autofs records.
    if [[ -d $candidate ]]; then ls "$candidate" >/dev/null; fi
    if [[ $(findmnt -rn -M "$candidate" -t vfat -o FSTYPE 2>/dev/null || true) == vfat ]]; then esp=$candidate; break; fi
  done
  [[ -n $esp ]] || fail 'no mounted FAT ESP found'
  export BOOTRECOV_ESP_DIR=$esp
  printf 'ESP=%q\n' "$esp" >>"$state/versions"
  # Use the fallback UEFI path, without changing host or guest NVRAM.
  log_run grub-install.log grub-install --target=x86_64-efi --efi-directory="$esp" --boot-directory=/boot --removable --no-nvram
  mkdir -p /etc/default/grub.d
  cat >/etc/default/grub <<'GRUB'
GRUB_DEFAULT=saved
GRUB_TIMEOUT=2
GRUB_DISABLE_OS_PROBER=true
GRUB_TERMINAL=serial
GRUB_SERIAL_COMMAND="serial --unit=0 --speed=115200"
GRUB_CMDLINE_LINUX="console=ttyS0,115200 net.ifnames=0"
GRUB
  # Cloud-image snippets may override the main file's unattended boot settings.
  cat >/etc/default/grub.d/99-bootrecov-vm.cfg <<'GRUB'
GRUB_DEFAULT=saved
GRUB_TIMEOUT_STYLE=menu
GRUB_TIMEOUT=2
GRUB_RECORDFAIL_TIMEOUT=2
GRUB_TERMINAL=serial
GRUB_SERIAL_COMMAND="serial --unit=0 --speed=115200"
GRUB
  # Isolate direct kernel and Windows paths. BootNext can select arbitrary
  # firmware loaders and is exercised separately as an unknown target below.
  if [[ -f /etc/grub.d/31_efi_bootnext ]]; then chmod -x /etc/grub.d/31_efi_bootnext; fi
  snapshot old "$A" linux
  cp -a /var/backups/bootrecov-snapshots/old /var/backups/bootrecov-snapshots/shared
  snapshot keep "$B" linux-lts
  esp_uuid=$(findmnt -rn -M "$esp" -t vfat -o UUID)
  [[ -n $esp_uuid ]] || fail 'ESP UUID is missing'
  cat >/etc/grub.d/42_bootrecov_windows_test <<WINDOWS
#!/bin/sh
cat <<'EOF'
menuentry 'Windows path fixture (not booted)' {
    insmod part_gpt
    insmod fat
    search --no-floppy --fs-uuid --set=root $esp_uuid
    chainloader /EFI/Microsoft/Boot/bootmgfw.efi
}
EOF
WINDOWS
  chmod +x /etc/grub.d/42_bootrecov_windows_test
  # Retain B's installed package ownership while letting Bootrecov create its
  # restoration marker through the production archive extraction path.
  mv "/usr/lib/modules/$B" /opt/bootrecov-B-original
  br backup activate keep
  assert_marked "$B"
  cp -a /opt/bootrecov-B-original/build "/usr/lib/modules/$B/build"
  rm -rf /opt/bootrecov-B-original
  log_run remove-A-packages.log pacman -R --noconfirm linux linux-headers
  [[ ! -e /usr/lib/modules/$A ]] || fail 'pacman left A modules behind before restoration'
  mkdir -p /usr/src/bootrecov_probe-1.0
  cat >/usr/src/bootrecov_probe-1.0/bootrecov_probe.c <<'MODULE'
#include <linux/module.h>
static int __init probe_start(void) { return 0; }
static void __exit probe_stop(void) { }
module_init(probe_start);
module_exit(probe_stop);
MODULE_LICENSE("GPL");
MODULE_DESCRIPTION("Disposable Bootrecov integration-test module");
MODULE
  printf 'obj-m += bootrecov_probe.o\n' >/usr/src/bootrecov_probe-1.0/Makefile
  cat >/usr/src/bootrecov_probe-1.0/dkms.conf <<'DKMS'
PACKAGE_NAME="bootrecov_probe"
PACKAGE_VERSION="1.0"
BUILT_MODULE_NAME[0]="bootrecov_probe"
DEST_MODULE_LOCATION[0]="/updates"
AUTOINSTALL="no"
MAKE[0]="make -C /lib/modules/${kernelver}/build M=${dkms_tree}/${PACKAGE_NAME}/${PACKAGE_VERSION}/build modules"
CLEAN="make -C /lib/modules/${kernelver}/build M=${dkms_tree}/${PACKAGE_NAME}/${PACKAGE_VERSION}/build clean"
DKMS
  log_run dkms-add.log dkms add -m bootrecov_probe -v 1.0
  log_run dkms-install-B.log dkms install -m bootrecov_probe -v 1.0 -k "$B"
  restore_old
  br backup activate shared
  assert_keep
  grub-script-check /boot/grub/grub.cfg
  KEEP_ID=$(entry_id keep)
  printf 'KEEP_ID=%q\n' "$KEEP_ID" >>"$state/versions"
  grub-reboot "$KEEP_ID"
  grub-editenv /boot/grub/grubenv list | grep -qx "next_entry=$KEEP_ID" || fail 'GRUB next_entry not recorded'
  touch "$state/prepared"
  echo "ARCH_CLEANUP_PREPARED A=$A B=$B; reboot into $KEEP_ID, then run verify"
  ;;
verify)
  [[ -f $state/prepared ]] || fail 'prepare has not completed'
  # This state is written by this root-owned script in the disposable VM.
  # shellcheck disable=SC1090
  source "$state/versions"
  export BOOTRECOV_ESP_DIR=$ESP
  [[ $(uname -r) == "$B" ]] || fail "expected real recovery kernel $B, running $(uname -r)"
  grep -qE "(^| )bootrecov_entry=$KEEP_ID( |$)" /proc/cmdline || fail 'guest did not boot the generated keep recovery entry'
  pacman -Q linux-lts linux-lts-headers >/dev/null
  for pkg in linux linux-headers; do
    if pacman -Q "$pkg" >/dev/null 2>&1; then fail "$pkg remains installed"; fi
  done
  assert_keep; assert_marked "$A"; assert_dkms
  br backup deactivate old
  assert_entry_absent old; assert_marked "$A"; assert_dkms; assert_keep
  echo 'PASS shared kernel retained after deactivation of one recovery'
  br backup delete shared
  assert_entry_absent shared; assert_absent "$A"; assert_no_dkms; assert_keep
  [[ ! -e /var/backups/bootrecov-snapshots/shared ]] || fail 'snapshot delete did not remove source'
  echo 'PASS real delete cleaned A and DKMS with Windows and distinct B recovery present'
  restore_old
  br backup deactivate old
  assert_absent "$A"; assert_no_dkms; assert_keep
  echo 'PASS real deactivate cleaned A and DKMS with Windows and distinct B recovery present'
  # Unrecognized targets must fail cleanup after removing the selected entry.
  for target in 'chainloader /EFI/BOOT/BOOTX64.EFI' 'chainloader /EFI/Linux/unknown.efi' 'chainloader $linux_loader' 'bootnext 0000'; do
    restore_old
    cat >/etc/grub.d/43_bootrecov_unknown_test <<UNKNOWN
#!/bin/sh
cat <<'EOF'
menuentry 'Unknown chainloader fixture' {
    $target
}
EOF
UNKNOWN
    chmod +x /etc/grub.d/43_bootrecov_unknown_test
    grub-mkconfig -o /boot/grub/grub.cfg >"$state/grub-unknown.log" 2>&1
    if br backup deactivate old >"$state/unknown-cleanup.log" 2>&1; then fail "unknown chainloader $target did not block cleanup"; fi
    assert_entry_absent old; assert_marked "$A"; assert_dkms; assert_keep
    grep -qiE 'EFI boot reference|unsupported GRUB command syntax|chainloader|BootNext|unsupported EFI reference' "$state/unknown-cleanup.log" || fail 'failure was unrelated to unknown chainloader'
    rm /etc/grub.d/43_bootrecov_unknown_test
    grub-mkconfig -o /boot/grub/grub.cfg >"$state/grub-known.log" 2>&1
    br reconcile
    assert_absent "$A"; assert_no_dkms; assert_keep
    echo "PASS conservative unknown chainloader: $target"
  done
  restore_old
  [[ ! -e /var/lib/pacman/db.lck ]] || fail 'unexpected package lock'
  ( set -o noclobber; printf '%s\n' 'bootrecov-vm-test' >/var/lib/pacman/db.lck )
  br backup deactivate old
  assert_entry_absent old; assert_marked "$A"; assert_dkms; assert_keep
  [[ $(cat /var/lib/pacman/db.lck) == bootrecov-vm-test ]] || fail 'package lock was changed'
  rm /var/lib/pacman/db.lck
  br reconcile
  assert_absent "$A"; assert_no_dkms; assert_keep
  echo 'PASS package transaction lock deferred cleanup'
  restore_old
  mkdir -p "/usr/lib/modules/$A/bootrecov-test-mount"
  mount -t tmpfs -o size=1m tmpfs "/usr/lib/modules/$A/bootrecov-test-mount"
  if br backup deactivate old >"$state/mount-cleanup.log" 2>&1; then fail 'mounted subtree did not block cleanup'; fi
  assert_entry_absent old; assert_marked "$A"; assert_dkms; assert_keep
  grep -qi 'mount' "$state/mount-cleanup.log" || fail 'failure was unrelated to mounted subtree'
  umount "/usr/lib/modules/$A/bootrecov-test-mount"
  br reconcile
  assert_absent "$A"; assert_no_dkms; assert_keep
  echo 'PASS mounted subtree blocked cleanup before DKMS mutation'
  restore_old
  mkdir -p /run/lock/bootrecov
  exec {lock_fd}>/run/lock/bootrecov/.bootrecov-operations.lock
  flock "$lock_fd"
  if timeout 2 /tmp/bootrecov backup deactivate old >"$state/process-lock-cleanup.log" 2>&1; then
    fail 'operation ignored the cross-process lock'
  else
    lock_status=$?
    [[ $lock_status == 124 ]] || fail "locked operation failed unexpectedly with $lock_status"
  fi
  grep -Fq -- "menuentry 'Bootrecov $BOOTRECOV_EFI_MIRROR_DIR/old'" /boot/grub/grub.cfg || fail 'locked operation mutated the entry'
  assert_marked "$A"; assert_dkms
  flock -u "$lock_fd"
  exec {lock_fd}>&-
  br backup deactivate old
  assert_absent "$A"; assert_no_dkms; assert_keep
  echo 'PASS cross-process operation lock serialized deactivation and cleanup'
  restore_old
  rm "/usr/lib/modules/$A/.bootrecov-restored"
  br backup delete old
  assert_tree "$A"; assert_dkms; assert_keep
  echo 'PASS unmarked module directory and its DKMS build retained'
  # Drop B's entry while actually running B. Both running-kernel and real
  # pacman ownership checks must retain the restored B module tree.
  br backup deactivate keep
  assert_entry_absent keep; assert_marked "$B"
  pacman -Qo "/usr/lib/modules/$B/kernel" >/dev/null
  echo 'PASS running installed B kernel retained after final recovery removal'
  grub-script-check /boot/grub/grub.cfg
  touch "$state/verified"
  echo "ARCH_CLEANUP_VERIFIED running=$B Windows/shared/distinct/unknown/DKMS/lock/unmarked passed"
  ;;
*) fail 'usage: guest_arch_cleanup.sh prepare|verify' ;;
esac
