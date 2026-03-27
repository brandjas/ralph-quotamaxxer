#!/usr/bin/env bash
# test-guard.sh — Unit tests for the guard subcommand.
set -euo pipefail

REPO_DIR="$(dirname "$(readlink -f "$0")")/.."
DATA_DIR="$(mktemp -d)"
PROXY_BIN="${PROXY_BIN:-}"

cleanup() { rm -rf "$DATA_DIR"; }
trap cleanup EXIT

echo "=== quotamaxxer guard tests ==="
echo "Data dir: $DATA_DIR"

# Build proxy if no binary specified.
if [[ -z "$PROXY_BIN" ]]; then
    echo "Building proxy..."
    PROXY_BIN="$DATA_DIR/quotamaxxer-proxy"
    (cd "$REPO_DIR/proxy" && go build -o "$PROXY_BIN" .)
fi

NOW=$(date +%s)
FIVE_HOUR_RESET=$(( NOW + 9000 ))   # halfway through 5h window
SEVEN_DAY_RESET=$(( NOW + 302400 )) # halfway through 7d window

# --- Test 1: No data files → exit 0 ---
echo ""
echo "--- Test 1: no data files → exit 0 ---"
if "$PROXY_BIN" guard --threshold-5h 0.8 --data-dir "$DATA_DIR" --quiet; then
    echo "PASS: guard exits 0 with no data"
else
    echo "FAIL: guard should exit 0 with no data"
    exit 1
fi

# --- Test 2: Low utilization (proxy format) → exit 0 ---
echo ""
echo "--- Test 2: low utilization (proxy format) → exit 0 ---"
cat > "$DATA_DIR/usage-proxy.json" <<EOF
{
  "source": "proxy",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "epoch": $NOW,
  "status": "allowed",
  "five_hour": { "utilization": 0.1, "reset": $FIVE_HOUR_RESET },
  "seven_day": { "utilization": 0.1, "reset": $SEVEN_DAY_RESET }
}
EOF

if "$PROXY_BIN" guard --threshold-5h 0.8 --threshold-7d 0.9 --data-dir "$DATA_DIR" --quiet; then
    echo "PASS: guard exits 0 with low utilization"
else
    echo "FAIL: guard should exit 0 with low utilization"
    exit 1
fi

# --- Test 3: High utilization + short timeout → exit 1 ---
echo ""
echo "--- Test 3: high utilization + timeout → exit 1 ---"
cat > "$DATA_DIR/usage-proxy.json" <<EOF
{
  "source": "proxy",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "epoch": $NOW,
  "status": "allowed",
  "five_hour": { "utilization": 0.9, "reset": $FIVE_HOUR_RESET },
  "seven_day": { "utilization": 0.1, "reset": $SEVEN_DAY_RESET }
}
EOF

if "$PROXY_BIN" guard --threshold-5h 0.5 --data-dir "$DATA_DIR" --wait-timeout 1s --quiet; then
    echo "FAIL: guard should exit 1 on timeout"
    exit 1
else
    echo "PASS: guard exits 1 on timeout"
fi

# --- Test 4: Statusline format → exit 0 ---
echo ""
echo "--- Test 4: statusline format → exit 0 ---"
rm -f "$DATA_DIR/usage-proxy.json"
cat > "$DATA_DIR/usage-statusline.json" <<EOF
{
  "source": "statusline",
  "timestamp": { "iso": "$(date -u +%Y-%m-%dT%H:%M:%SZ)", "epoch": $NOW },
  "rate_limits": {
    "five_hour": { "used_pct": 10, "resets_at": $FIVE_HOUR_RESET, "burn_ratio": 0.2 },
    "seven_day": { "used_pct": 10, "resets_at": $SEVEN_DAY_RESET, "burn_ratio": 0.2 }
  }
}
EOF

if "$PROXY_BIN" guard --threshold-5h 0.8 --data-dir "$DATA_DIR" --quiet; then
    echo "PASS: guard reads statusline format"
else
    echo "FAIL: guard should read statusline format"
    exit 1
fi

# --- Test 5: --source proxy ignores statusline file ---
echo ""
echo "--- Test 5: --source proxy ignores statusline file ---"
# Only statusline file exists (proxy file was removed above).
if "$PROXY_BIN" guard --threshold-5h 0.8 --data-dir "$DATA_DIR" --source proxy --quiet; then
    echo "PASS: --source proxy ignores statusline, exits 0 (no data)"
else
    echo "FAIL: --source proxy with no proxy file should exit 0"
    exit 1
fi

# --- Test 6: Most recent file wins ---
echo ""
echo "--- Test 6: most recent file wins ---"
# Proxy file: old, high utilization. Statusline file: newer, low utilization.
OLD_EPOCH=$(( NOW - 60 ))
cat > "$DATA_DIR/usage-proxy.json" <<EOF
{
  "source": "proxy",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "epoch": $OLD_EPOCH,
  "five_hour": { "utilization": 0.95, "reset": $FIVE_HOUR_RESET },
  "seven_day": { "utilization": 0.95, "reset": $SEVEN_DAY_RESET }
}
EOF
# Statusline file already has low utilization with epoch $NOW (newer).

if "$PROXY_BIN" guard --threshold-5h 0.5 --threshold-7d 0.5 --data-dir "$DATA_DIR" --wait-timeout 1s --quiet; then
    echo "PASS: newer statusline data wins over older proxy data"
else
    echo "FAIL: should use newer statusline data (low util) over older proxy data (high util)"
    exit 1
fi

# --- Test 7: --help flag works ---
echo ""
echo "--- Test 7: help output ---"
if "$PROXY_BIN" help | grep -q "quotamaxxer"; then
    echo "PASS: help output contains 'quotamaxxer'"
else
    echo "FAIL: help output missing"
    exit 1
fi

echo ""
echo "=== All guard tests passed ==="
