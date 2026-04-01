#!/usr/bin/env bash
#
# Generate assets/monitor.png — macOS-style screenshot of `quotamaxxer monitor`.
# Works inside or outside tmux. Requires: tmux, freeze, rsvg-convert.
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${QUOTAMAXXER_BIN:-$HOME/.claude/ralph-quotamaxxer/bin/quotamaxxer}"
SESSION="qm-screenshot-$$"

cleanup() { tmux kill-session -t "$SESSION" 2>/dev/null || true; }
trap cleanup EXIT

tmux new-session -d -s "$SESSION" -x 80 -y 24 "$BIN monitor"
tmux resize-window -t "$SESSION" -x 80 -y 24
sleep 3

mkdir -p "$REPO_ROOT/assets"
tmux capture-pane -t "$SESSION" -ep | freeze -c full --output "$REPO_ROOT/assets/monitor.svg"
rsvg-convert "$REPO_ROOT/assets/monitor.svg" -o "$REPO_ROOT/assets/monitor.png" -z 1.5
echo "wrote assets/monitor.png"
