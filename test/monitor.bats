#!/usr/bin/env bats
# monitor.bats — tests for the monitor subcommand.

load helpers

@test "monitor: exits cleanly on piped q" {
    echo q | "$QM_BIN" monitor
}

@test "monitor: --source validates input" {
    run "$QM_BIN" monitor --source invalid < /dev/null
    [ "$status" -ne 0 ]
    [[ "$output" == *"invalid --source"* ]]
}

@test "monitor: accepts --source proxy" {
    echo q | "$QM_BIN" monitor --source proxy
}

@test "monitor: accepts --source statusline" {
    echo q | "$QM_BIN" monitor --source statusline
}

@test "monitor: accepts --data-dir flag" {
    echo q | "$QM_BIN" monitor --data-dir "$DATA_DIR"
}

@test "monitor: renders chart labels and stats with proxy data" {
    local utils=(0.1 0.2 0.3 0.4 0.5)
    for i in 0 1 2 3 4; do
        epoch=$(( NOW - 300 * (4 - i) ))
        cat >> "$DATA_DIR/usage-history.jsonl" <<EOF
{"source":"proxy","epoch":${epoch},"five_hour":{"utilization":${utils[$i]},"reset":${FIVE_HOUR_RESET}},"seven_day":{"utilization":${utils[$i]},"reset":${SEVEN_DAY_RESET}}}
EOF
    done
    write_proxy_json 0.5 0.25
    output=$(echo q | "$QM_BIN" monitor --data-dir "$DATA_DIR" 2>&1)
    [[ "$output" == *"5-Hour Utilization"* ]]
    [[ "$output" == *"7-Day Utilization"* ]]
    [[ "$output" == *"5h:"* ]]
    [[ "$output" == *"7d:"* ]]
    [[ "$output" == *"q quit"* ]]
}

@test "monitor: renders chart labels with statusline data" {
    for i in $(seq 1 3); do
        epoch=$(( NOW - 600 * (3 - i) ))
        pct5h=$(( i * 10 ))
        pct7d=$(( i * 5 ))
        cat >> "$DATA_DIR/usage-history.jsonl" <<EOF
{"source":"statusline","timestamp":{"epoch":${epoch}},"rate_limits":{"five_hour":{"used_pct":${pct5h}},"seven_day":{"used_pct":${pct7d}}}}
EOF
    done
    write_statusline_json 30 15 0.6 0.3
    output=$(echo q | "$QM_BIN" monitor --data-dir "$DATA_DIR" --source statusline 2>&1)
    [[ "$output" == *"5-Hour Utilization"* ]]
    [[ "$output" == *"7-Day Utilization"* ]]
    [[ "$output" == *"5h:"* ]]
}

@test "monitor: handles mixed proxy and statusline history" {
    # Proxy record older
    epoch_proxy=$(( NOW - 600 ))
    cat >> "$DATA_DIR/usage-history.jsonl" <<EOF
{"source":"proxy","epoch":${epoch_proxy},"five_hour":{"utilization":0.3,"reset":${FIVE_HOUR_RESET}},"seven_day":{"utilization":0.1,"reset":${SEVEN_DAY_RESET}}}
EOF
    # Statusline record newer
    epoch_sl=$(( NOW - 300 ))
    cat >> "$DATA_DIR/usage-history.jsonl" <<EOF
{"source":"statusline","timestamp":{"epoch":${epoch_sl}},"rate_limits":{"five_hour":{"used_pct":40},"seven_day":{"used_pct":12}}}
EOF
    write_statusline_json 40 12 0.8 0.4
    output=$(echo q | "$QM_BIN" monitor --data-dir "$DATA_DIR" --source both 2>&1)
    [[ "$output" == *"5-Hour Utilization"* ]]
    [[ "$output" == *"7-Day Utilization"* ]]
}

@test "monitor: does not crash without data files" {
    local empty_dir
    empty_dir="$(mktemp -d)"
    output=$(echo q | "$QM_BIN" monitor --data-dir "$empty_dir" 2>&1)
    [[ "$output" == *"5-Hour Utilization"* ]]
    rm -rf "$empty_dir"
}

@test "monitor: does not crash with empty history file" {
    touch "$DATA_DIR/usage-history.jsonl"
    echo q | "$QM_BIN" monitor --data-dir "$DATA_DIR"
}

@test "monitor: survives malformed lines in history" {
    echo "not valid json" >> "$DATA_DIR/usage-history.jsonl"
    echo '{"source":"proxy","epoch":'"$NOW"',"five_hour":{"utilization":0.2,"reset":'"$FIVE_HOUR_RESET"'},"seven_day":{"utilization":0.1,"reset":'"$SEVEN_DAY_RESET"'}}' >> "$DATA_DIR/usage-history.jsonl"
    echo "{truncated" >> "$DATA_DIR/usage-history.jsonl"
    write_proxy_json 0.2 0.1
    output=$(echo q | "$QM_BIN" monitor --data-dir "$DATA_DIR" 2>&1)
    [[ "$output" == *"5-Hour Utilization"* ]]
}

@test "monitor: shows in help output" {
    run "$QM_BIN" --help
    [[ "$output" == *"monitor"* ]]
    [[ "$output" == *"Live TUI dashboard"* ]]
}
