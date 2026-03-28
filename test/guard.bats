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

@test "guard: most recent file wins (newer low-util beats older high-util)" {
    OLD_EPOCH=$(( NOW - 60 ))
    write_proxy_json 0.95 0.95 "$OLD_EPOCH"
    write_statusline_json 10 10 0.2 0.2 "$NOW"
    run "$QM_BIN" guard --threshold-5h 0.5 --threshold-7d 0.5 --data-dir "$DATA_DIR" --source both --wait-timeout 1s --quiet
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
