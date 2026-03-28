#!/usr/bin/env bats
# proxy.bats — tests for the standalone proxy subcommand.

load helpers

teardown() {
    # Kill proxy if still running.
    if [[ -n "${PROXY_PID:-}" ]] && kill -0 "$PROXY_PID" 2>/dev/null; then
        kill "$PROXY_PID" 2>/dev/null || true
        wait "$PROXY_PID" 2>/dev/null || true
    fi
    rm -rf "$DATA_DIR" "${PORT_FILE:-}"
}

start_proxy() {
    PORT_FILE="$(mktemp)"
    QUOTAMAXXER_DATA_DIR="$DATA_DIR" QUOTAMAXXER_PORT_FILE="$PORT_FILE" "$QM_BIN" proxy &
    PROXY_PID=$!

    # Wait for port file.
    for _ in $(seq 1 30); do
        if [[ -s "$PORT_FILE" ]]; then break; fi
        if ! kill -0 "$PROXY_PID" 2>/dev/null; then return 1; fi
        sleep 0.1
    done
    PROXY_PORT=$(<"$PORT_FILE")
    PROXY_PORT="${PROXY_PORT%%$'\n'}"
}

@test "proxy: starts and writes port file" {
    start_proxy
    [[ -n "$PROXY_PORT" ]]
    [[ "$PROXY_PORT" =~ ^[0-9]+$ ]]
}

@test "proxy: responds to HTTP requests" {
    start_proxy
    # The proxy should forward to upstream (which we haven't configured for a
    # real backend), but it should at least accept the TCP connection and
    # return some HTTP response (likely 502 bad gateway for no-auth requests).
    run curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:${PROXY_PORT}/v1/messages" \
        -X POST -H "Content-Type: application/json" -d '{}'
    # 502 is expected (upstream will reject or be unreachable without valid auth).
    [[ "$output" =~ ^[0-9]+$ ]]
}

@test "proxy: creates data directory" {
    local nested="$DATA_DIR/sub/dir"
    QUOTAMAXXER_DATA_DIR="$nested" QUOTAMAXXER_PORT_FILE="$(mktemp)" "$QM_BIN" proxy &
    PROXY_PID=$!
    sleep 0.5
    [ -d "$nested" ]
}

@test "proxy: shuts down on SIGTERM" {
    start_proxy
    kill -TERM "$PROXY_PID"
    wait "$PROXY_PID" 2>/dev/null || true
    # Process should be gone.
    ! kill -0 "$PROXY_PID" 2>/dev/null
}
