#!/usr/bin/env bash
# wrapper.sh — Launch quotamaxxer-proxy on an OS-assigned port, then exec claude.
#
# Usage:
#   quotamaxxer [flags] [-- claude-args...]    Start proxy, optionally guard, then run claude
#   quotamaxxer guard [flags]                  Wait for rate limits, then exit
#
# Flags (before --):
#   --threshold-5h <ratio>   Wait until 5h burn ratio drops below this
#   --threshold-7d <ratio>   Wait until 7d burn ratio drops below this
#   --wait-timeout <dur>     Max guard wait time (e.g. 30m, 1h)
#   --run-timeout <dur>      Max claude run time (e.g. 2h)
#   --source <src>           Data source: both (default), proxy, statusline
#   --quiet                  Suppress guard output
#   --help                   Show help
#
# Without --, all arguments are forwarded to claude as-is.
set -euo pipefail

PROXY_BIN="${QUOTAMAXXER_PROXY_BIN:-$HOME/.claude/ralph-quotamaxxer/bin/quotamaxxer-proxy}"
DATA_DIR="${QUOTAMAXXER_DATA_DIR:-$HOME/.claude/ralph-quotamaxxer/data}"

if [[ ! -x "$PROXY_BIN" ]]; then
    echo "quotamaxxer: proxy binary not found at $PROXY_BIN" >&2
    echo "Run install.sh first, or set QUOTAMAXXER_PROXY_BIN." >&2
    exit 1
fi

mkdir -p "$DATA_DIR"

# --- Argument parsing ---
# Split on first "--": before = quotamaxxer args, after = claude args.
# No "--" = all args go to claude.
QUOTAMAXXER_ARGS=()
CLAUDE_ARGS=()
found_separator=false

for arg in "$@"; do
    if [[ "$found_separator" == false && "$arg" == "--" ]]; then
        found_separator=true
        continue
    fi
    if [[ "$found_separator" == true ]]; then
        CLAUDE_ARGS+=("$arg")
    else
        QUOTAMAXXER_ARGS+=("$arg")
    fi
done

if [[ "$found_separator" == false ]]; then
    CLAUDE_ARGS=("$@")
    QUOTAMAXXER_ARGS=()
fi

# --- Parse quotamaxxer args ---
THRESHOLD_5H=""
THRESHOLD_7D=""
WAIT_TIMEOUT=""
RUN_TIMEOUT=""
SOURCE=""
QUIET=""
STANDALONE_GUARD=false

i=0
while (( i < ${#QUOTAMAXXER_ARGS[@]} )); do
    case "${QUOTAMAXXER_ARGS[$i]}" in
        guard)
            STANDALONE_GUARD=true
            # Pass remaining args to the guard subcommand.
            GUARD_ARGS=("${QUOTAMAXXER_ARGS[@]:$((i+1))}")
            break
            ;;
        --threshold-5h)
            (( i++ )) || true
            THRESHOLD_5H="${QUOTAMAXXER_ARGS[$i]}"
            ;;
        --threshold-7d)
            (( i++ )) || true
            THRESHOLD_7D="${QUOTAMAXXER_ARGS[$i]}"
            ;;
        --wait-timeout)
            (( i++ )) || true
            WAIT_TIMEOUT="${QUOTAMAXXER_ARGS[$i]}"
            ;;
        --run-timeout)
            (( i++ )) || true
            RUN_TIMEOUT="${QUOTAMAXXER_ARGS[$i]}"
            ;;
        --source)
            (( i++ )) || true
            SOURCE="${QUOTAMAXXER_ARGS[$i]}"
            ;;
        --quiet)
            QUIET=1
            ;;
        --help|-h)
            exec "$PROXY_BIN" help
            ;;
        *)
            echo "quotamaxxer: unknown flag '${QUOTAMAXXER_ARGS[$i]}'" >&2
            echo "Run 'quotamaxxer --help' for usage." >&2
            exit 1
            ;;
    esac
    (( i++ )) || true
done

# --- Standalone guard mode ---
if [[ "$STANDALONE_GUARD" == true ]]; then
    GUARD_CMD=("$PROXY_BIN" guard --data-dir "$DATA_DIR")
    [[ ${#GUARD_ARGS[@]} -gt 0 ]] && GUARD_CMD+=("${GUARD_ARGS[@]}")
    exec "${GUARD_CMD[@]}"
fi

# --- Run guard before proxy+claude if thresholds set ---
if [[ -n "$THRESHOLD_5H" || -n "$THRESHOLD_7D" ]]; then
    GUARD_CMD=("$PROXY_BIN" guard --data-dir "$DATA_DIR")
    [[ -n "$THRESHOLD_5H" ]] && GUARD_CMD+=(--threshold-5h "$THRESHOLD_5H")
    [[ -n "$THRESHOLD_7D" ]] && GUARD_CMD+=(--threshold-7d "$THRESHOLD_7D")
    [[ -n "$WAIT_TIMEOUT" ]] && GUARD_CMD+=(--wait-timeout "$WAIT_TIMEOUT")
    [[ -n "$SOURCE" ]] && GUARD_CMD+=(--source "$SOURCE")
    [[ -n "$QUIET" ]] && GUARD_CMD+=(--quiet)
    "${GUARD_CMD[@]}" || exit $?
fi

# --- Start proxy ---
PORT_FILE=$(mktemp)

cleanup() {
    kill "$PROXY_PID" 2>/dev/null || true
    wait "$PROXY_PID" 2>/dev/null || true
    rm -f "$PORT_FILE"
}
trap cleanup EXIT

QUOTAMAXXER_DATA_DIR="$DATA_DIR" QUOTAMAXXER_PORT_FILE="$PORT_FILE" "$PROXY_BIN" >/dev/null 2>&1 &
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

if [[ -n "$RUN_TIMEOUT" ]]; then
    exec timeout "$RUN_TIMEOUT" claude "${CLAUDE_ARGS[@]}"
else
    exec claude "${CLAUDE_ARGS[@]}"
fi
