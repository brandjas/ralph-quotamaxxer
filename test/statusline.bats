#!/usr/bin/env bats
# statusline.bats — tests for the statusline subcommand.

load helpers

STATUSLINE_INPUT='{
  "model": { "display_name": "Haiku 1.5", "id": "claude-3-5-haiku-20241022" },
  "session_id": "test-session",
  "context_window": { "used_percentage": 42.5 },
  "cost": { "total_cost_usd": 0.12 },
  "rate_limits": {
    "five_hour": { "used_percentage": 25.0, "resets_at": FIVE_HOUR_RESET },
    "seven_day": { "used_percentage": 10.0, "resets_at": SEVEN_DAY_RESET }
  }
}'

build_input() {
    echo "$STATUSLINE_INPUT" \
        | sed "s/FIVE_HOUR_RESET/$FIVE_HOUR_RESET/g" \
        | sed "s/SEVEN_DAY_RESET/$SEVEN_DAY_RESET/g"
}

@test "statusline: outputs formatted line with model, context, cost" {
    run bash -c 'build_input | "$1" statusline' -- "$QM_BIN"
    # Fallback: inline the function since bats subshell won't see it.
    local input
    input=$(build_input)
    run bash -c 'echo "$1" | QUOTAMAXXER_DATA_DIR="$2" "$3" statusline' -- "$input" "$DATA_DIR" "$QM_BIN"
    [ "$status" -eq 0 ]
    [[ "$output" == *"Haiku 1.5"* ]]
    [[ "$output" == *"Ctx 43%"* ]]   # 42.5 rounds to 43
    [[ "$output" == *'$0.12'* ]]
    [[ "$output" == *"5h:"* ]]
    [[ "$output" == *"7d:"* ]]
}

@test "statusline: writes usage-statusline.json" {
    local input
    input=$(build_input)
    run bash -c 'echo "$1" | QUOTAMAXXER_DATA_DIR="$2" "$3" statusline' -- "$input" "$DATA_DIR" "$QM_BIN"
    [ "$status" -eq 0 ]
    [ -f "$DATA_DIR/usage-statusline.json" ]
    # Validate key fields.
    run jq -e '.source == "statusline"' "$DATA_DIR/usage-statusline.json"
    [ "$status" -eq 0 ]
    run jq -e '.model.id == "claude-3-5-haiku-20241022"' "$DATA_DIR/usage-statusline.json"
    [ "$status" -eq 0 ]
}

@test "statusline: appends to usage-history.jsonl" {
    local input
    input=$(build_input)
    run bash -c 'echo "$1" | QUOTAMAXXER_DATA_DIR="$2" "$3" statusline' -- "$input" "$DATA_DIR" "$QM_BIN"
    [ "$status" -eq 0 ]
    [ -f "$DATA_DIR/usage-history.jsonl" ]
    local lines
    lines=$(wc -l < "$DATA_DIR/usage-history.jsonl")
    [ "$lines" -ge 1 ]
}

@test "statusline: invalid JSON on stdin exits 1" {
    run bash -c 'echo "not json" | QUOTAMAXXER_DATA_DIR="$1" "$2" statusline' -- "$DATA_DIR" "$QM_BIN"
    [ "$status" -eq 1 ]
    [[ "$output" == *"parse JSON"* ]]
}

@test "statusline: input without rate_limits still outputs basic line" {
    local input='{"model":{"display_name":"Opus","id":"opus"},"session_id":"s","context_window":{"used_percentage":50},"cost":{"total_cost_usd":1.23}}'
    run bash -c 'echo "$1" | QUOTAMAXXER_DATA_DIR="$2" "$3" statusline' -- "$input" "$DATA_DIR" "$QM_BIN"
    [ "$status" -eq 0 ]
    [[ "$output" == *"Opus"* ]]
    [[ "$output" == *"Ctx 50%"* ]]
    [[ "$output" == *'$1.23'* ]]
    # No rate limit section.
    [[ "$output" != *"5h:"* ]]
}
