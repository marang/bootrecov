#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORK_DIR="${ROOT_DIR}/test/bootvm/work"
BIN_PATH="${ROOT_DIR}/bin/bootrecov"
SMOKE_BIN="${ROOT_DIR}/bin/guest_smoke"
SMOKE_SRC="${ROOT_DIR}/test/bootvm/guest_smoke.go"
BASE_IMAGE="${WORK_DIR}/ubuntu-noble-server-cloudimg-amd64.img"
OVERLAY_IMAGE="${WORK_DIR}/bootvm-overlay.qcow2"
SEED_IMAGE="${WORK_DIR}/seed.img"
SERIAL_LOG="${WORK_DIR}/serial.log"
PID_FILE="${WORK_DIR}/qemu.pid"
OVMF_CODE_FILE="${WORK_DIR}/OVMF_CODE.fd"
OVMF_VARS_FILE="${WORK_DIR}/OVMF_VARS.fd"
SSH_KEY="${WORK_DIR}/id_ed25519"
SSH_PORT="${BOOTVM_SSH_PORT:-2222}"
SSH_CHECK_TIMEOUT="${BOOTVM_SSH_CHECK_TIMEOUT:-8}"
VM_USER="bootrecov"
VM_HOST="127.0.0.1"
SSH_OPTS=(-i "${SSH_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 -o ConnectionAttempts=1 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 -p "${SSH_PORT}")
SCP_OPTS=(-i "${SSH_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 -P "${SSH_PORT}")
IMAGE_URL="${BOOTVM_IMAGE_URL:-https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img}"
CHECK_ONLY="${1:-}"
RUN_LOG="${WORK_DIR}/run.log"
STATUS_FILE="${WORK_DIR}/status"
LAST_ERROR_FILE="${WORK_DIR}/last_error"
PREPARED_MARKER="${WORK_DIR}/.prepared"
SSH_PORT_FILE="${WORK_DIR}/ssh_port"

missing=()

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    missing+=("$1")
  fi
}

check_prereqs() {
  missing=()
  require_cmd qemu-system-x86_64
  require_cmd qemu-img
  require_cmd ssh
  require_cmd scp
  require_cmd ssh-keygen
  require_cmd timeout
  require_cmd curl
  if ! command -v cloud-localds >/dev/null 2>&1 && ! command -v genisoimage >/dev/null 2>&1; then
    missing+=("cloud-localds|genisoimage")
  fi

  if [[ ${#missing[@]} -gt 0 ]]; then
    echo "missing required tools for test-bootvm:" >&2
    for m in "${missing[@]}"; do
      echo "  - ${m}" >&2
    done
    echo >&2
    echo "Arch package hints:" >&2
    echo "  sudo pacman -S --needed qemu-base openssh curl cloud-image-utils" >&2
    echo "  # alternatively: sudo pacman -S --needed qemu-base openssh curl cdrkit" >&2
    return 1
  fi
  echo "preflight OK: all required tools are installed."
}

find_ovmf_code() {
  local candidates=(
    /usr/share/edk2-ovmf/x64/OVMF_CODE.fd
    /usr/share/edk2-ovmf/x64/OVMF_CODE.4m.fd
    /usr/share/edk2/ovmf/OVMF_CODE.fd
    /usr/share/edk2/ovmf/OVMF_CODE.4m.fd
    /usr/share/edk2/x64/OVMF_CODE.fd
    /usr/share/edk2/x64/OVMF_CODE.4m.fd
    /usr/share/OVMF/OVMF_CODE.fd
    /usr/share/OVMF/OVMF_CODE.4m.fd
    /usr/share/ovmf/OVMF_CODE.fd
    /usr/share/ovmf/OVMF_CODE.4m.fd
    /usr/share/qemu/OVMF_CODE.fd
    /usr/share/qemu/OVMF_CODE.4m.fd
  )
  local f
  for f in "${candidates[@]}"; do
    if [[ -f "${f}" ]]; then
      echo "${f}"
      return 0
    fi
  done
  find /usr/share -path '*ovmf*' \( -iname 'OVMF_CODE*.fd' -o -iname 'ovmf_code*.bin' \) -print -quit 2>/dev/null
}

find_ovmf_vars() {
  local candidates=(
    /usr/share/edk2-ovmf/x64/OVMF_VARS.fd
    /usr/share/edk2-ovmf/x64/OVMF_VARS.4m.fd
    /usr/share/edk2/ovmf/OVMF_VARS.fd
    /usr/share/edk2/ovmf/OVMF_VARS.4m.fd
    /usr/share/edk2/x64/OVMF_VARS.fd
    /usr/share/edk2/x64/OVMF_VARS.4m.fd
    /usr/share/OVMF/OVMF_VARS.fd
    /usr/share/OVMF/OVMF_VARS.4m.fd
    /usr/share/ovmf/OVMF_VARS.fd
    /usr/share/ovmf/OVMF_VARS.4m.fd
    /usr/share/qemu/OVMF_VARS.fd
    /usr/share/qemu/OVMF_VARS.4m.fd
  )
  local f
  for f in "${candidates[@]}"; do
    if [[ -f "${f}" ]]; then
      echo "${f}"
      return 0
    fi
  done
  find /usr/share -path '*ovmf*' \( -iname 'OVMF_VARS*.fd' -o -iname 'ovmf_vars*.bin' \) -print -quit 2>/dev/null
}

prepare_ovmf() {
  local code_template vars_template
  code_template="$(find_ovmf_code || true)"
  vars_template="$(find_ovmf_vars || true)"
  if [[ -z "${code_template}" || -z "${vars_template}" ]]; then
    echo "OVMF firmware files not found. Install 'edk2-ovmf' or 'ovmf'." >&2
    return 1
  fi
  cp -f "${code_template}" "${OVMF_CODE_FILE}"
  cp -f "${vars_template}" "${OVMF_VARS_FILE}"
}

set_status() {
  local state="$1"
  printf "%s %s\n" "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" "${state}" >"${STATUS_FILE}"
}

log_qemu_state() {
  local label="$1"
  if [[ ! -f "${PID_FILE}" ]]; then
    echo "[qemu] ${label}: no pid file"
    return
  fi
  local pid
  pid="$(cat "${PID_FILE}" 2>/dev/null || true)"
  if [[ -z "${pid}" ]]; then
    echo "[qemu] ${label}: empty pid file"
    return
  fi
  if kill -0 "${pid}" 2>/dev/null; then
    echo "[qemu] ${label}: pid=${pid} alive"
  else
    echo "[qemu] ${label}: pid=${pid} dead"
  fi
}

timestamp_stream() {
  local line
  while IFS= read -r line || [[ -n "${line}" ]]; do
    printf "%s %s\n" "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" "${line}"
  done
}

port_in_use() {
  local p="$1"
  if command -v ss >/dev/null 2>&1; then
    ss -ltn "sport = :${p}" 2>/dev/null | tail -n +2 | grep -q .
    return $?
  fi
  if command -v lsof >/dev/null 2>&1; then
    lsof -iTCP:"${p}" -sTCP:LISTEN >/dev/null 2>&1
    return $?
  fi
  return 1
}

choose_ssh_port() {
  local p
  for p in "${SSH_PORT}" 2222 2223 2224 2225 2200 2201 2202 2203; do
    if ! port_in_use "${p}"; then
      SSH_PORT="${p}"
      SSH_OPTS=(-i "${SSH_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 -o ConnectionAttempts=1 -o ServerAliveInterval=5 -o ServerAliveCountMax=2 -p "${SSH_PORT}")
      SCP_OPTS=(-i "${SSH_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 -P "${SSH_PORT}")
      echo "${SSH_PORT}" >"${SSH_PORT_FILE}"
      echo "using SSH forward port: ${SSH_PORT}"
      return 0
    fi
  done
  echo "no free local SSH forward port found (tried 2200-2203 and 2222-2225)" >&2
  return 1
}

wait_for_ssh() {
  local timeout_s="${1:-360}"
  local deadline=$((SECONDS + timeout_s))
  while (( SECONDS < deadline )); do
    if ! qemu_alive; then
      echo "qemu died while waiting for SSH." >&2
      if [[ -f /tmp/bootrecov-qemu.log ]]; then
        echo "last qemu log lines:" >&2
        tail -n 50 /tmp/bootrecov-qemu.log >&2 || true
      fi
      return 1
    fi
    if ssh_probe; then
      return 0
    fi
    sleep 1
  done
  return 1
}

qemu_alive() {
  if [[ ! -f "${PID_FILE}" ]]; then
    return 1
  fi
  local pid
  pid="$(cat "${PID_FILE}" 2>/dev/null || true)"
  [[ -n "${pid}" ]] || return 1
  [[ "${pid}" =~ ^[0-9]+$ ]] || return 1
  kill -0 "${pid}" 2>/dev/null || return 1

  # Guard against stale PID reuse, but tolerate distro-specific qemu binary names.
  local args
  args="$(ps -p "${pid}" -o args= 2>/dev/null || true)"
  [[ -n "${args}" ]] || return 1
  [[ "${args}" == *"qemu-system"* ]] || return 1
  [[ "${args}" == *"${OVERLAY_IMAGE}"* ]] || return 1

  return 0
}

ssh_probe() {
  timeout --foreground "${SSH_CHECK_TIMEOUT}s" ssh "${SSH_OPTS[@]}" "${VM_USER}@${VM_HOST}" "echo ok" >/dev/null 2>&1
}

launch_qemu() {
  rm -f "${PID_FILE}"
  qemu-system-x86_64 \
    -name bootrecov-bootvm \
    -m 2048 \
    -smp 2 \
    -machine q35 \
    -drive "if=pflash,format=raw,readonly=on,file=${OVMF_CODE_FILE}" \
    -drive "if=pflash,format=raw,file=${OVMF_VARS_FILE}" \
    -drive "file=${OVERLAY_IMAGE},if=virtio,format=qcow2" \
    -drive "file=${SEED_IMAGE},if=virtio,format=raw" \
    -netdev "user,id=n1,hostfwd=tcp::${SSH_PORT}-:22" \
    -device virtio-net-pci,netdev=n1 \
    -display none \
    -monitor none \
    -no-shutdown \
    -serial "file:${SERIAL_LOG}" \
    -pidfile "${PID_FILE}" \
    -daemonize >/tmp/bootrecov-qemu.log 2>&1
}

relaunch_qemu_after_reboot() {
  local i
  for i in $(seq 1 60); do
    if launch_qemu >/tmp/bootrecov-qemu-relaunch.log 2>&1; then
      echo "qemu relaunched after reboot."
      log_qemu_state "after-relaunch"
      return 0
    fi
    sleep 1
  done
  echo "failed to relaunch qemu after reboot; last error:" >&2
  tail -n 20 /tmp/bootrecov-qemu-relaunch.log >&2 || true
  return 1
}

reboot_and_wait() {
  set_status "rebooting-guest"
  log_qemu_state "before-reboot-request"
  echo "requesting guest reboot..."
  ssh "${SSH_OPTS[@]}" "${VM_USER}@${VM_HOST}" "sudo systemctl reboot" >/dev/null 2>&1 || true
  log_qemu_state "after-reboot-request"

  # Wait until SSH goes down first.
  for _ in $(seq 1 60); do
    if ! ssh_probe; then
      break
    fi
    sleep 1
  done

  set_status "waiting-for-ssh-after-reboot"
  if ! qemu_alive; then
    echo "qemu exited after reboot request; attempting controlled relaunch..."
    relaunch_qemu_after_reboot
  fi
  log_qemu_state "before-ssh-wait-after-reboot"
  if ! wait_for_ssh 360; then
    if ! qemu_alive; then
      echo "qemu died during reboot wait; attempting controlled relaunch..."
      if relaunch_qemu_after_reboot && wait_for_ssh 180; then
        log_qemu_state "after-relaunch-ssh-return"
        echo "guest reachable after qemu relaunch."
        return 0
      fi
    fi
    echo "guest did not return after reboot (or qemu exited)" >&2
    log_qemu_state "failed-ssh-wait-after-reboot"
    echo "inspect serial log tail for last reboot lines: ${SERIAL_LOG}" >&2
    return 1
  fi
  log_qemu_state "after-ssh-return-after-reboot"
  echo "guest is reachable again after reboot."
}

ensure_assets() {
  mkdir -p "${WORK_DIR}"
  if [[ ! -f "${BASE_IMAGE}" ]]; then
    set_status "downloading-base-image"
    echo "downloading cloud image: ${IMAGE_URL}"
    curl -L --fail -o "${BASE_IMAGE}" "${IMAGE_URL}"
  fi
  if [[ ! -f "${SSH_KEY}" ]]; then
    set_status "generating-ssh-key"
    ssh-keygen -t ed25519 -N "" -f "${SSH_KEY}" >/dev/null
  fi
}

cleanup() {
  if [[ -f "${PID_FILE}" ]]; then
    local pid
    pid="$(cat "${PID_FILE}" 2>/dev/null || true)"
    if [[ -n "${pid}" ]] && kill -0 "${pid}" >/dev/null 2>&1; then
      kill "${pid}" >/dev/null 2>&1 || true
      sleep 1
      kill -9 "${pid}" >/dev/null 2>&1 || true
    fi
  fi
}

on_exit() {
  local code="$1"
  if [[ "${code}" -eq 0 ]]; then
    set_status "finished"
    rm -f "${LAST_ERROR_FILE}"
  else
    set_status "failed"
    echo "run failed (exit ${code})" >"${LAST_ERROR_FILE}"
  fi
  cleanup
}

mkdir -p "${WORK_DIR}"
rm -f "${RUN_LOG}" "${LAST_ERROR_FILE}"
set_status "starting"
exec > >(timestamp_stream | tee -a "${RUN_LOG}") 2>&1
trap 'on_exit $?' EXIT

check_prereqs

if [[ "${CHECK_ONLY}" == "--check" ]]; then
  set_status "preflight-ok"
  exit 0
fi

if [[ "${CHECK_ONLY}" == "--prepare" ]]; then
  set_status "preparing-assets"
  ensure_assets
  date -u +'%Y-%m-%dT%H:%M:%SZ' >"${PREPARED_MARKER}"
  set_status "prepared"
  echo "prepare complete: cached base image + ssh key are ready."
  exit 0
fi

choose_ssh_port

if [[ ! -f "${PREPARED_MARKER}" ]]; then
  echo "first run: performing automatic prepare step..."
  set_status "preparing-assets"
  ensure_assets
  date -u +'%Y-%m-%dT%H:%M:%SZ' >"${PREPARED_MARKER}"
fi

if [[ ! -x "${BIN_PATH}" || "${ROOT_DIR}/cmd/bootrecov/main.go" -nt "${BIN_PATH}" || "${ROOT_DIR}/internal/tui/backups.go" -nt "${BIN_PATH}" || "${ROOT_DIR}/internal/tui/model.go" -nt "${BIN_PATH}" || "${ROOT_DIR}/internal/tui/view.go" -nt "${BIN_PATH}" ]]; then
  set_status "building-binary"
  (cd "${ROOT_DIR}" && make build)
fi

if [[ ! -x "${SMOKE_BIN}" || "${SMOKE_SRC}" -nt "${SMOKE_BIN}" || "${ROOT_DIR}/internal/tui/backups.go" -nt "${SMOKE_BIN}" || "${ROOT_DIR}/internal/tui/model.go" -nt "${SMOKE_BIN}" || "${ROOT_DIR}/internal/tui/view.go" -nt "${SMOKE_BIN}" ]]; then
  set_status "building-guest-smoke-binary"
  (cd "${ROOT_DIR}" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "${SMOKE_BIN}" ./test/bootvm/guest_smoke.go)
fi

set_status "preparing-disks"
rm -f "${OVERLAY_IMAGE}" "${SEED_IMAGE}" "${SERIAL_LOG}" "${PID_FILE}" "${OVMF_CODE_FILE}" "${OVMF_VARS_FILE}"
qemu-img create -f qcow2 -F qcow2 -b "${BASE_IMAGE}" "${OVERLAY_IMAGE}" >/dev/null
prepare_ovmf

set_status "writing-cloud-init"
cat >"${WORK_DIR}/user-data" <<EOF
#cloud-config
users:
  - name: ${VM_USER}
    groups: [sudo]
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - $(cat "${SSH_KEY}.pub")
runcmd:
  - [ mkdir, -p, /boot/efi/bootrecov-snapshots ]
EOF

cat >"${WORK_DIR}/meta-data" <<EOF
instance-id: bootrecov-bootvm
local-hostname: bootrecov-bootvm
EOF

if command -v cloud-localds >/dev/null 2>&1; then
  set_status "creating-seed-image"
  cloud-localds "${SEED_IMAGE}" "${WORK_DIR}/user-data" "${WORK_DIR}/meta-data"
elif command -v genisoimage >/dev/null 2>&1; then
  set_status "creating-seed-image"
  genisoimage -output "${SEED_IMAGE}" -volid cidata -joliet -rock "${WORK_DIR}/user-data" "${WORK_DIR}/meta-data" >/dev/null 2>&1
else
  echo "need one of: cloud-localds or genisoimage" >&2
  exit 1
fi

set_status "starting-qemu"
launch_qemu
log_qemu_state "after-initial-launch"

set_status "waiting-for-ssh"
echo "waiting for ssh on ${VM_HOST}:${SSH_PORT} ..."
for _ in $(seq 1 180); do
  if ssh_probe; then
    break
  fi
  sleep 2
done

if ! ssh_probe; then
  echo "guest ssh did not become ready; see ${SERIAL_LOG}" >&2
  exit 1
fi

set_status "copying-binary"
scp "${SCP_OPTS[@]}" "${BIN_PATH}" "${VM_USER}@${VM_HOST}:/tmp/bootrecov" >/dev/null
scp "${SCP_OPTS[@]}" "${SMOKE_BIN}" "${VM_USER}@${VM_HOST}:/tmp/guest_smoke" >/dev/null

set_status "running-guest-smoke-test"
echo "guest smoke test: begin"
ssh "${SSH_OPTS[@]}" "${VM_USER}@${VM_HOST}" "bash -se" <<'EOF'
set -euo pipefail
BACKUP_DIR=/boot/efi/bootrecov-snapshots/2026-smoke
SNAPSHOT_DIR=/var/backups/bootrecov-snapshots/2026-smoke
GRUB_CUSTOM=/etc/grub.d/41_bootrecov_snapshots

print_bootrecov_entries() {
  local file="$1"
  local tag="$2"
  if [[ ! -f "${file}" ]]; then
    echo "[${tag}] <file-missing>"
    return
  fi
  if ! sudo grep -q "menuentry 'Bootrecov " "${file}"; then
    echo "[${tag}] <no-bootrecov-entries>"
    return
  fi
  sudo awk '
    /menuentry '\''Bootrecov / {inblk=1}
    inblk {print}
    inblk && /^EOF$/ {inblk=0; print "---"}
  ' "${file}" | sed "s/^/[${tag}] /"
}

echo "[guest] collecting boot artifacts"
KERNEL_SRC="$(readlink -f /vmlinuz || true)"
INITRD_SRC="$(readlink -f /initrd.img || true)"
if [[ -z "${KERNEL_SRC}" || ! -f "${KERNEL_SRC}" ]]; then
  KERNEL_SRC="$(ls -1 /boot/vmlinuz-* 2>/dev/null | head -n1 || true)"
fi
if [[ -z "${INITRD_SRC}" || ! -f "${INITRD_SRC}" ]]; then
  INITRD_SRC="$(ls -1 /boot/initrd.img-* 2>/dev/null | head -n1 || true)"
fi
if [[ -z "${KERNEL_SRC}" || -z "${INITRD_SRC}" ]]; then
  echo "[guest] failed to locate kernel/initrd sources" >&2
  exit 1
fi
echo "[guest] setup start"
sudo chmod +x /tmp/bootrecov
sudo mkdir -p "${BACKUP_DIR}"
sudo mkdir -p "${SNAPSHOT_DIR}"
sudo mkdir -p /etc/grub.d
if [[ ! -f "${GRUB_CUSTOM}" ]]; then
  sudo sh -c 'printf "#!/bin/bash\n" > /etc/grub.d/41_bootrecov_snapshots'
fi
sudo chmod 755 /etc/grub.d/41_bootrecov_snapshots
sudo cp -f "${KERNEL_SRC}" "${BACKUP_DIR}/vmlinuz"
sudo cp -f "${INITRD_SRC}" "${BACKUP_DIR}/initrd.img"
sudo cp -f "${KERNEL_SRC}" "${SNAPSHOT_DIR}/vmlinuz"
sudo cp -f "${INITRD_SRC}" "${SNAPSHOT_DIR}/initrd.img"
echo "[guest] setup done"

echo "[guest] grub before (bootrecov entries):"
print_bootrecov_entries "${GRUB_CUSTOM}" "grub-before"

echo "[guest] running deterministic grub smoke helper"
sudo chmod +x /tmp/guest_smoke
if ! timeout 45s sudo /tmp/guest_smoke "${BACKUP_DIR}" vmlinuz initrd.img >/tmp/bootrecov-smoke.log 2>&1; then
  rc=$?
  echo "[guest] smoke helper failed with exit ${rc}"
  sudo cat /tmp/bootrecov-smoke.log || true
  exit "${rc}"
fi
sudo cat /tmp/bootrecov-smoke.log || true
echo "[guest] smoke helper finished"

echo "[guest] grub after (bootrecov entries):"
print_bootrecov_entries "${GRUB_CUSTOM}" "grub-after"

sudo grep -q "bootrecov-" "${GRUB_CUSTOM}"
echo "[guest] grub entry check passed"
EOF
echo "guest smoke test: done"

set_status "extracting-entry-id"
ENTRY_ID="$(awk -F= '/ADDED_ID=/{print $2}' test/bootvm/work/run.log | tail -n1 | tr -d '\r\n')"
if [[ -z "${ENTRY_ID}" ]]; then
  echo "failed to extract ADDED_ID from run log" >&2
  exit 1
fi
echo "detected backup entry id: ${ENTRY_ID}"

set_status "preparing-grub-reboot"
ssh "${SSH_OPTS[@]}" "${VM_USER}@${VM_HOST}" "bash -se" <<EOF
set -euo pipefail
if ! command -v grub-mkconfig >/dev/null 2>&1; then
  sudo DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=180 install -y --no-install-recommends grub-common >/tmp/bootrecov-grub.log 2>&1
fi
sudo grub-mkconfig -o /boot/grub/grub.cfg >/tmp/bootrecov-grubcfg.log 2>&1
sudo grub-reboot '${ENTRY_ID}'
sudo grub-editenv list | sed 's/^/[grubenv] /'
echo "[grubcfg] bootrecov entry excerpt for ${ENTRY_ID}:"
if ! sudo awk -v id="${ENTRY_ID}" '
  index(\$0, "menuentry ") {show=0}
  index(\$0, id) {show=1}
  show {print}
  show && /^}/ {show=0}
' /boot/grub/grub.cfg | sed 's/^/[grubcfg] /'; then
  echo "[grubcfg] <failed-to-read>"
fi
EOF

set_status "reboot-test-1"
reboot_and_wait
CMDLINE_1="$(ssh "${SSH_OPTS[@]}" "${VM_USER}@${VM_HOST}" "cat /proc/cmdline" | tr -d '\r')"
echo "post-reboot #1 cmdline: ${CMDLINE_1}"
if [[ "${CMDLINE_1}" != *"bootrecov_entry=${ENTRY_ID}"* ]]; then
  echo "expected boot marker bootrecov_entry=${ENTRY_ID} not found after reboot #1" >&2
  exit 1
fi
echo "reboot #1 verified: booted backup GRUB entry."

set_status "corrupting-primary-boot"
ssh "${SSH_OPTS[@]}" "${VM_USER}@${VM_HOST}" "bash -se" <<'EOF'
set -euo pipefail
KERNEL_SRC="$(readlink -f /vmlinuz || true)"
if [[ -z "${KERNEL_SRC}" || ! -f "${KERNEL_SRC}" ]]; then
  KERNEL_SRC="$(ls -1 /boot/vmlinuz-* 2>/dev/null | head -n1 || true)"
fi
if [[ -z "${KERNEL_SRC}" || ! -f "${KERNEL_SRC}" ]]; then
  echo "could not locate primary kernel to corrupt" >&2
  exit 1
fi
sudo cp -f "${KERNEL_SRC}" "${KERNEL_SRC}.bootrecov.bak"
sudo truncate -s 0 "${KERNEL_SRC}"
echo "corrupted primary kernel at ${KERNEL_SRC}"
EOF

set_status "reboot-test-2"
ssh "${SSH_OPTS[@]}" "${VM_USER}@${VM_HOST}" "sudo grub-reboot '${ENTRY_ID}'" || true
reboot_and_wait
CMDLINE_2="$(ssh "${SSH_OPTS[@]}" "${VM_USER}@${VM_HOST}" "cat /proc/cmdline" | tr -d '\r')"
echo "post-reboot #2 cmdline: ${CMDLINE_2}"
if [[ "${CMDLINE_2}" != *"bootrecov_entry=${ENTRY_ID}"* ]]; then
  echo "expected boot marker bootrecov_entry=${ENTRY_ID} not found after reboot #2" >&2
  exit 1
fi
echo "reboot #2 verified: backup entry boots even after primary kernel corruption."

set_status "passed"
echo "bootvm test passed."
echo "serial log: ${SERIAL_LOG}"
