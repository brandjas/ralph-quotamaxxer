#!/usr/bin/env bats
# persistent.bats — tests for the opt-in persistent proxy daemon.
# Uses --claude-command to substitute mock commands instead of real claude.
# End-to-end coverage with real Claude Code is in test/e2e.

load helpers

setup() {
    DATA_DIR="$(mktemp -d)"
    export QUOTAMAXXER_DATA_DIR="$DATA_DIR"
    # A port of our own, so tests never touch a real daemon on the default one.
    export QUOTAMAXXER_PERSISTENT_PORT=$(( 20000 + RANDOM % 10000 ))
    unset QUOTAMAXXER_PERSISTENT ANTHROPIC_BASE_URL
    BASE="http://127.0.0.1:$QUOTAMAXXER_PERSISTENT_PORT"
}

teardown() {
    "$QM_BIN" daemon stop >/dev/null 2>&1 || true
    if [[ -n "${SQUATTER_PID:-}" ]]; then kill "$SQUATTER_PID" 2>/dev/null || true; fi
    rm -rf "$DATA_DIR"
}

@test "persistent: off by default, nothing is left running" {
    run "$QM_BIN" --claude-command bash -- -c 'echo "$ANTHROPIC_BASE_URL"'
    [ "$status" -eq 0 ]
    [[ "$output" != "$BASE"* ]]
    run "$QM_BIN" daemon status
    [ "$status" -eq 3 ]
    [[ "$output" == *"not running"* ]]
}

@test "persistent: the env var routes the child through the daemon, which outlives the run" {
    QUOTAMAXXER_PERSISTENT=1 run "$QM_BIN" --claude-command bash -- -c 'echo "$ANTHROPIC_BASE_URL $QUOTAMAXXER_PROXIED_PORT"'
    [ "$status" -eq 0 ]
    [[ "$output" == "$BASE $QUOTAMAXXER_PERSISTENT_PORT" ]]
    run "$QM_BIN" daemon status
    [ "$status" -eq 0 ]
    [[ "$output" == *"running on 127.0.0.1:$QUOTAMAXXER_PERSISTENT_PORT"* ]]
}

@test "persistent: --persistent flag works like the env var" {
    run "$QM_BIN" --persistent --claude-command bash -- -c 'echo "$ANTHROPIC_BASE_URL"'
    [ "$status" -eq 0 ]
    [[ "$output" == "$BASE" ]]
}

@test "persistent: an inherited upstream is carried in the URL" {
    ANTHROPIC_BASE_URL=https://gateway.example/anthropic \
        run "$QM_BIN" --persistent --claude-command bash -- -c 'echo "$ANTHROPIC_BASE_URL"'
    [ "$status" -eq 0 ]
    [[ "$output" == "$BASE/_quotamaxxer/u/"* ]]
}

@test "persistent: a launch from inside a proxied session keeps its URL" {
    "$QM_BIN" daemon start
    ANTHROPIC_BASE_URL="$BASE" \
        run "$QM_BIN" --persistent --claude-command bash -- -c 'echo "$ANTHROPIC_BASE_URL"'
    [ "$status" -eq 0 ]
    [[ "$output" == "$BASE" ]]
}

@test "persistent: daemon start is idempotent and stop frees the port" {
    run "$QM_BIN" daemon start
    [ "$status" -eq 0 ]
    pid1="$("$QM_BIN" daemon status | sed -n 's/.*pid \([0-9]*\).*/\1/p')"
    "$QM_BIN" daemon start
    pid2="$("$QM_BIN" daemon status | sed -n 's/.*pid \([0-9]*\).*/\1/p')"
    [[ -n "$pid1" && "$pid1" == "$pid2" ]]

    run "$QM_BIN" daemon stop
    [ "$status" -eq 0 ]
    run "$QM_BIN" daemon status
    [ "$status" -eq 3 ]
}

@test "persistent: the control endpoints need the key" {
    "$QM_BIN" daemon start
    run curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/_quotamaxxer/shutdown"
    [[ "$output" == 403 ]]
    run curl -s -o /dev/null -w '%{http_code}' "$BASE/_quotamaxxer/u/00/aHR0cDovL2V4YW1wbGU/v1/messages"
    [[ "$output" == 403 ]]
    run "$QM_BIN" daemon status
    [ "$status" -eq 0 ]
}

@test "persistent: the statusline of a proxied session revives a dead daemon" {
    "$QM_BIN" daemon start
    kill -9 "$("$QM_BIN" daemon status | sed -n 's/.*pid \([0-9]*\).*/\1/p')"
    run "$QM_BIN" daemon status
    [ "$status" -eq 3 ]

    echo '{}' | QUOTAMAXXER_PROXIED_PORT="$QUOTAMAXXER_PERSISTENT_PORT" "$QM_BIN" statusline >/dev/null
    for _ in $(seq 1 50); do
        if "$QM_BIN" daemon status >/dev/null; then break; fi
        sleep 0.1
    done
    run "$QM_BIN" daemon status
    [ "$status" -eq 0 ]
}

@test "persistent: the statusline of an ordinary session starts nothing" {
    echo '{}' | "$QM_BIN" statusline >/dev/null
    sleep 0.3
    run "$QM_BIN" daemon status
    [ "$status" -eq 3 ]
}

@test "persistent: a taken port falls back to the default behaviour with a warning" {
    # A daemon for another data dir stands in for "someone else's process".
    OTHER_DIR="$(mktemp -d)"
    QUOTAMAXXER_DATA_DIR="$OTHER_DIR" "$QM_BIN" daemon run 2>/dev/null &
    SQUATTER_PID=$!
    for _ in $(seq 1 50); do
        if curl -s -o /dev/null "$BASE/_quotamaxxer/health"; then break; fi
        sleep 0.1
    done

    run "$QM_BIN" --persistent --claude-command bash -- -c 'echo "url=$ANTHROPIC_BASE_URL"'
    [ "$status" -eq 0 ]
    [[ "$output" == *"persistent proxy unavailable"* ]]
    # bats has no terminal, so the fallback is the per-run proxy on another port.
    [[ "$output" == *"url=http://127.0.0.1:"* ]]
    [[ "$output" != *"url=$BASE"* ]]

    # We can't stop a daemon we don't hold the key for.
    run "$QM_BIN" daemon stop
    [ "$status" -ne 0 ]
    kill -0 "$SQUATTER_PID"
    rm -rf "$OTHER_DIR"
}
