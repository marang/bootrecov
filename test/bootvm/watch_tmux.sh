#!/usr/bin/env bash
set -euo pipefail

SESSION="${1:-bootrecov-bootvm}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNNER="${ROOT_DIR}/test/bootvm/run_rootless_vm_test.sh"
SERIAL_LOG="${ROOT_DIR}/test/bootvm/work/serial.log"
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
tmux split-window -h -t "${SESSION}:bootvm.0"
if [[ "${RIGHT_PANE_MODE}" == "serial" ]]; then
  tmux send-keys -t "${SESSION}:bootvm.1" "cd \"${ROOT_DIR}\"; clear; mkdir -p test/bootvm/work; touch \"${SERIAL_LOG}\"; tail -f \"${SERIAL_LOG}\"" C-m
else
  tmux send-keys -t "${SESSION}:bootvm.1" "cd \"${ROOT_DIR}\"; clear; mkdir -p test/bootvm/work; touch \"${RUN_LOG}\" \"${SERIAL_LOG}\" \"${STATUS_FILE}\"; watch -n 1 'printf \"=== status ===\\n\"; cat \"${STATUS_FILE}\" 2>/dev/null || true; printf \"\\n=== run.log (tail) ===\\n\"; tail -n 20 \"${RUN_LOG}\" 2>/dev/null || true; printf \"\\n=== serial.log (tail) ===\\n\"; tail -n 20 \"${SERIAL_LOG}\" 2>/dev/null || true'" C-m
fi

tmux select-layout -t "${SESSION}:bootvm" even-horizontal
tmux set-option -t "${SESSION}" remain-on-exit on

echo "tmux session created: ${SESSION}"
echo "Attach with:"
echo "  tmux attach -t ${SESSION}"
exec tmux attach -t "${SESSION}"
