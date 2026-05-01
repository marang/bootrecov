#!/usr/bin/env bash
set -euo pipefail

SESSION="${1:-bootrecov-bootvm}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNNER="${ROOT_DIR}/test/bootvm/run_rootless_vm_test.sh"
SERIAL_LOG="${ROOT_DIR}/test/bootvm/work/serial.log"
SERIAL_SOCKET="${ROOT_DIR}/test/bootvm/work/serial.sock"
RUN_LOG="${ROOT_DIR}/test/bootvm/work/run.log"
STATUS_FILE="${ROOT_DIR}/test/bootvm/work/status"
RIGHT_PANE_MODE="${BOOTVM_RIGHT_PANE_MODE:-dashboard}"

if ! command -v tmux >/dev/null 2>&1; then
  echo "tmux is required for this target. Install tmux and retry." >&2
  exit 1
fi

if [[ ! -x "${RUNNER}" ]]; then
  echo "bootvm runner script missing or not executable: ${RUNNER}" >&2
  exit 1
fi

if tmux has-session -t "${SESSION}" 2>/dev/null; then
  tmux kill-session -t "${SESSION}"
fi

tmux new-session -d -s "${SESSION}" -n bootvm

# Pane 0: full rootless VM test run
tmux send-keys -t "${SESSION}:bootvm.0" "cd \"${ROOT_DIR}\"; clear; bash test/bootvm/run_rootless_vm_test.sh" C-m

# Pane 1: combined status + runner + serial output
tmux split-window -h -p 55 -t "${SESSION}:bootvm.0"
if [[ "${RIGHT_PANE_MODE}" == "serial" ]]; then
  tmux send-keys -t "${SESSION}:bootvm.1" "cd \"${ROOT_DIR}\"; clear; mkdir -p test/bootvm/work; touch \"${SERIAL_LOG}\"; tail -f \"${SERIAL_LOG}\"" C-m
else
  tmux send-keys -t "${SESSION}:bootvm.1" "cd \"${ROOT_DIR}\"; clear; mkdir -p test/bootvm/work; touch \"${RUN_LOG}\" \"${SERIAL_LOG}\" \"${STATUS_FILE}\"; watch -n 1 'printf \"=== status ===\\n\"; cat \"${STATUS_FILE}\" 2>/dev/null || true; printf \"\\n=== runner activity ===\\n\"; pgrep -af \"run_rootless_vm_test|qemu-system|socat\" | sed \"s/^/host: /\" || true; printf \"\\n=== run.log (tail) ===\\n\"; tail -n 32 \"${RUN_LOG}\" 2>/dev/null || true; printf \"\\n=== serial.log (tail) ===\\n\"; tail -n 14 \"${SERIAL_LOG}\" 2>/dev/null || true'" C-m
fi

# Pane 2: interactive serial console into the guest, reconnecting across reboots.
tmux split-window -v -p 45 -t "${SESSION}:bootvm.1"
tmux send-keys -t "${SESSION}:bootvm.2" "cd \"${ROOT_DIR}\"; clear; echo 'Waiting for VM serial console. This pane reconnects after guest reboots.'; while true; do if [[ -S \"${SERIAL_SOCKET}\" ]]; then echo \"Connecting to ${SERIAL_SOCKET} ...\"; socat -,rawer,escape=0x1d UNIX-CONNECT:\"${SERIAL_SOCKET}\"; echo 'Serial console disconnected; waiting to reconnect ...'; fi; sleep 2; done" C-m

tmux set-option -t "${SESSION}" remain-on-exit on
tmux select-pane -t "${SESSION}:bootvm.0"

echo "tmux session created: ${SESSION}"
echo "Attach with:"
echo "  tmux attach -t ${SESSION}"
exec tmux attach -t "${SESSION}"
