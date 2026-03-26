#!/usr/bin/env bash
# wrapper.sh — Launch ralph-quotamaxxer/proxy on an OS-assigned port, then exec claude.
# Usage: wrapper.sh [claude args...]
#   e.g. wrapper.sh -p "hello"
#        wrapper.sh          (interactive)
set -euo pipefail

PROXY_BIN="${QUOTAMAXXER_PROXY_BIN:-$HOME/.claude/ralph-quotamaxxer/bin/ralph-quotamaxxer/proxy}"
DATA_DIR="${QUOTAMAXXER_DATA_DIR:-$HOME/.claude/ralph-quotamaxxer/data}"

if [[ ! -x "$PROXY_BIN" ]]; then
    echo "quotamaxxer: proxy binary not found at $PROXY_BIN" >&2
    echo "Run install.sh first, or set QUOTAMAXXER_PROXY_BIN." >&2
    exit 1
fi

mkdir -p "$DATA_DIR"

# Temp file for the proxy to write its OS-assigned port.
PORT_FILE=$(mktemp)

cleanup() {
    kill "$PROXY_PID" 2>/dev/null || true
    wait "$PROXY_PID" 2>/dev/null || true
    rm -f "$PORT_FILE"
}
trap cleanup EXIT

# Start proxy. Port 0 = OS picks an ephemeral port, writes it to PORT_FILE.
QUOTAMAXXER_DATA_DIR="$DATA_DIR" QUOTAMAXXER_PORT_FILE="$PORT_FILE" "$PROXY_BIN" 2>/dev/null &
PROXY_PID=$!

# Wait for the port file to be written (up to 3s).
for _ in $(seq 1 30); do
    if [[ -s "$PORT_FILE" ]]; then
        break
    fi
    if ! kill -0 "$PROXY_PID" 2>/dev/null; then
        echo "quotamaxxer: proxy exited unexpectedly" >&2
        exit 1
    fi
    sleep 0.1
done

PORT=$(<"$PORT_FILE")
PORT="${PORT%%$'\n'}"  # trim newline

if [[ -z "$PORT" ]]; then
    echo "quotamaxxer: proxy did not report its port" >&2
    exit 1
fi

export ANTHROPIC_BASE_URL="http://127.0.0.1:$PORT"
exec claude "$@"
