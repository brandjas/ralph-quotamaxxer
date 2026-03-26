#!/usr/bin/env bash
# test-proxy.sh — Smoke test: start proxy, fire a trivial Haiku call, verify usage-proxy.json.
set -euo pipefail

REPO_DIR="$(dirname "$(readlink -f "$0")")/.."
DATA_DIR="$(mktemp -d)"
PORT_FILE="$(mktemp)"
PROXY_BIN="${PROXY_BIN:-}"

cleanup() {
    if [[ -n "${PROXY_PID:-}" ]] && kill -0 "$PROXY_PID" 2>/dev/null; then
        kill "$PROXY_PID" 2>/dev/null || true
        wait "$PROXY_PID" 2>/dev/null || true
    fi
    rm -rf "$DATA_DIR" "$PORT_FILE"
}
trap cleanup EXIT

echo "=== ralph-quotamaxxer/proxy smoke test ==="
echo "Data dir: $DATA_DIR"

# Build proxy if no binary specified.
if [[ -z "$PROXY_BIN" ]]; then
    echo "Building proxy..."
    PROXY_BIN="$DATA_DIR/ralph-quotamaxxer/proxy"
    (cd "$REPO_DIR/proxy" && go build -o "$PROXY_BIN" .)
fi

# Start proxy on OS-assigned ephemeral port.
echo "Starting proxy..."
QUOTAMAXXER_DATA_DIR="$DATA_DIR" QUOTAMAXXER_PORT_FILE="$PORT_FILE" "$PROXY_BIN" &
PROXY_PID=$!

# Wait for port file to be written.
for _ in $(seq 1 30); do
    if [[ -s "$PORT_FILE" ]]; then break; fi
    if ! kill -0 "$PROXY_PID" 2>/dev/null; then
        echo "FAIL: proxy exited early"
        exit 1
    fi
    sleep 0.1
done

PORT=$(<"$PORT_FILE")
PORT="${PORT%%$'\n'}"
echo "Proxy is up on 127.0.0.1:$PORT (PID $PROXY_PID)."

# Fire a trivial Haiku call through the proxy.
echo "Sending claude -p 'respond with only: ok' --model haiku..."
ANTHROPIC_BASE_URL="http://127.0.0.1:${PORT}" claude -p "respond with only the word: ok" --model haiku 2>/dev/null || true
echo ""

# Give the async writer a moment.
sleep 0.5

# Check usage-proxy.json.
RATELIMITS="$DATA_DIR/usage-proxy.json"
if [[ ! -f "$RATELIMITS" ]]; then
    echo "FAIL: $RATELIMITS was not created"
    echo "Contents of data dir:"
    ls -la "$DATA_DIR"
    exit 1
fi

echo "=== usage-proxy.json ==="
cat "$RATELIMITS"
echo ""

# Validate JSON structure.
if ! jq -e '.timestamp' "$RATELIMITS" > /dev/null 2>&1; then
    echo "FAIL: usage-proxy.json missing timestamp field"
    exit 1
fi

if ! jq -e '.raw_headers | length > 0' "$RATELIMITS" > /dev/null 2>&1; then
    echo "FAIL: usage-proxy.json has no raw_headers"
    exit 1
fi

if ! jq -e '.source == "proxy"' "$RATELIMITS" > /dev/null 2>&1; then
    echo "FAIL: usage-proxy.json missing source field"
    exit 1
fi

HEADER_COUNT=$(jq '.raw_headers | length' "$RATELIMITS")
echo "PASS: usage-proxy.json written with $HEADER_COUNT raw headers (source: proxy)"
echo ""

# Show parsed fields.
echo "Parsed fields:"
jq '{source, status, representative_claim, five_hour, seven_day, overage}' "$RATELIMITS"
echo ""

# Check usage-history.jsonl.
HISTORY="$DATA_DIR/usage-history.jsonl"
if [[ ! -f "$HISTORY" ]]; then
    echo "FAIL: $HISTORY was not created"
    exit 1
fi

LINE_COUNT=$(wc -l < "$HISTORY")
echo "=== usage-history.jsonl ($LINE_COUNT lines) ==="
# Show last line pretty-printed.
tail -1 "$HISTORY" | jq .
echo ""

if ! tail -1 "$HISTORY" | jq -e '.source == "proxy"' > /dev/null 2>&1; then
    echo "FAIL: history record missing source field"
    exit 1
fi

echo "PASS: usage-history.jsonl has $LINE_COUNT record(s) with source: proxy"

echo ""
echo "=== All checks passed ==="
