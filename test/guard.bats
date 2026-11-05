#!/usr/bin/env bats
# guard.bats — tests for the guard subcommand.

load helpers

@test "guard: no data files exits 0" {
    run "$QM_BIN" guard --threshold-5h 0.8 --data-dir "$DATA_DIR" --quiet
    [ "$status" -eq 0 ]
}

@test "guard: low utilization (proxy format) exits 0" {
    write_proxy_json 0.1 0.1
    run "$QM_BIN" guard --threshold-5h 0.8 --threshold-7d 0.9 --data-dir "$DATA_DIR" --quiet
    [ "$status" -eq 0 ]
}

@test "guard: high utilization + short timeout exits 1" {
    write_proxy_json 0.9 0.1
    run "$QM_BIN" guard --threshold-5h 0.5 --data-dir "$DATA_DIR" --wait-timeout 1s --quiet
    [ "$status" -eq 1 ]
}

@test "guard: reads statusline format" {
    write_statusline_json 10 10 0.2 0.2
    run "$QM_BIN" guard --threshold-5h 0.8 --data-dir "$DATA_DIR" --source statusline --quiet
    [ "$status" -eq 0 ]
}

@test "guard: --source proxy ignores statusline file" {
    write_statusline_json 10 10 0.2 0.2
    # No proxy file exists — guard should see no data and exit 0.
    run "$QM_BIN" guard --threshold-5h 0.8 --data-dir "$DATA_DIR" --source proxy --quiet
    [ "$status" -eq 0 ]
}

@test "guard: --source statusline ignores proxy file" {
    write_proxy_json 0.9 0.9
    # No statusline file exists — guard should see no data and exit 0.
    run "$QM_BIN" guard --threshold-5h 0.5 --data-dir "$DATA_DIR" --source statusline --quiet
    [ "$status" -eq 0 ]
}

@test "guard: a newer window (later reset) supersedes the previous window" {
    # Proxy saw the previous 5h window at 95%; it has since reset.
    cat > "$DATA_DIR/usage-proxy.json" <<JSON
{"source":"proxy","epoch":$(( NOW - 3600 )),
 "five_hour":{"utilization":0.95,"reset":$(( NOW - 600 ))},
 "seven_day":{"utilization":0.1,"reset":${SEVEN_DAY_RESET}}}
JSON
    write_statusline_json 10 10 0.2 0.2
    run "$QM_BIN" guard --threshold-5h 0.5 --data-dir "$DATA_DIR" --source both --wait-timeout 1s --quiet
    [ "$status" -eq 0 ]
}

@test "guard: missing both thresholds exits 1" {
    write_proxy_json 0.9 0.9
    run "$QM_BIN" guard --data-dir "$DATA_DIR" --quiet
    [ "$status" -eq 1 ]
    [[ "$output" == *"required"* ]]
}

@test "guard: only 7d threshold triggers on high 7d burn" {
    write_proxy_json 0.1 0.9
    run "$QM_BIN" guard --threshold-7d 0.5 --data-dir "$DATA_DIR" --wait-timeout 1s --quiet
    [ "$status" -eq 1 ]
}

@test "guard: invalid --source value exits 1" {
    run "$QM_BIN" guard --threshold-5h 0.8 --source bogus --data-dir "$DATA_DIR"
    [ "$status" -eq 1 ]
    [[ "$output" == *"invalid"* ]]
}

@test "guard: --wait-timeout with invalid duration exits non-zero" {
    run "$QM_BIN" guard --threshold-5h 0.8 --wait-timeout nope --data-dir "$DATA_DIR"
    [ "$status" -ne 0 ]
}

@test "guard: honors QUOTAMAXXER_DATA_DIR without --data-dir" {
    write_proxy_json 0.9 0.1   # into $DATA_DIR, which helpers exports as QUOTAMAXXER_DATA_DIR
    run "$QM_BIN" guard --threshold-5h 0.5 --wait-timeout 1s --quiet
    [ "$status" -eq 1 ]
    [[ "$output" == *"timeout"* ]]
}

@test "guard: --data-dir overrides QUOTAMAXXER_DATA_DIR" {
    write_proxy_json 0.9 0.1
    local empty
    empty="$(mktemp -d)"
    run "$QM_BIN" guard --threshold-5h 0.5 --wait-timeout 1s --data-dir "$empty"
    rm -rf "$empty"
    [ "$status" -eq 0 ]
    [[ "$output" == *"no usage data"* ]]
}

@test "guard: default source reads statusline data" {
    write_statusline_json 90 10 1.8 0.2
    run "$QM_BIN" guard --threshold-5h 0.5 --wait-timeout 1s --quiet
    [ "$status" -eq 1 ]
    [[ "$output" == *"timeout"* ]]
}

@test "guard: a stale statusline redraw can't hide higher usage in the same window" {
    # An idle session re-renders old, lower numbers stamped "now"; the proxy
    # saw higher utilization in the same window earlier. Usage only grows
    # within a window, so the higher value is the current one.
    write_proxy_json 0.9 0.1 "$(( NOW - 600 ))"
    write_statusline_json 10 10 0.2 0.2
    run "$QM_BIN" guard --threshold-5h 0.5 --wait-timeout 1s --quiet
    [ "$status" -eq 1 ]
}

@test "guard: a snapshot missing a window doesn't hide the other source's data" {
    write_proxy_json 0.1 0.95 "$(( NOW - 600 ))"
    # Newer statusline snapshot without a seven_day window (persisted as zeros).
    cat > "$DATA_DIR/usage-statusline.json" <<JSON
{"source":"statusline","timestamp":{"epoch":${NOW}},
 "rate_limits":{"five_hour":{"used_pct":10,"resets_at":${FIVE_HOUR_RESET}},
                "seven_day":{"used_pct":0,"resets_at":0}}}
JSON
    run "$QM_BIN" guard --threshold-7d 0.9 --wait-timeout 1s --quiet
    [ "$status" -eq 1 ]
    [[ "$output" == *"7d burn"* ]]
}
