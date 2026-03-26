#!/usr/bin/env bash
# parse.sh — Data layer: read stdin JSON, extract fields, compute burn ratios.
# Sourced by statusline.sh. Provides parse_stdin which sets shell variables.

readonly WINDOW_5H=18000
readonly WINDOW_7D=604800

_burn_ratio() {
    local used="$1" resets_at="$2" now="$3" duration="$4"
    awk -v used="$used" -v resets="$resets_at" -v now="$now" -v dur="$duration" \
        'BEGIN {
            elapsed = (dur - (resets - now)) / dur * 100
            if (elapsed < 0.1) elapsed = 0.1
            printf "%.2f", used / elapsed
        }'
}

parse_stdin() {
    local raw
    raw=$(cat)

    local fields
    fields=$(printf '%s' "$raw" | jq -r '
        [
            .model.display_name // "",
            .model.id // "",
            .session_id // "",
            (.context_window.used_percentage // 0 | tostring),
            (.cost.total_cost_usd // 0 | tostring),
            (if .rate_limits.five_hour.resets_at then "true" else "false" end),
            (.rate_limits.five_hour.used_percentage // 0 | tostring),
            (.rate_limits.five_hour.resets_at // 0 | tostring),
            (.rate_limits.seven_day.used_percentage // 0 | tostring),
            (.rate_limits.seven_day.resets_at // 0 | tostring)
        ] | @tsv
    ') || return 1

    IFS=$'\t' read -r \
        MODEL_NAME MODEL_ID SESSION_ID CONTEXT_PCT COST_USD \
        HAS_RATE_LIMITS \
        LIMIT_5H_USED_PCT LIMIT_5H_RESETS_AT \
        LIMIT_7D_USED_PCT LIMIT_7D_RESETS_AT \
        <<< "$fields"

    BURN_5H=""
    BURN_7D=""

    if [[ "$HAS_RATE_LIMITS" == "true" ]]; then
        local now
        now=$(date +%s)
        BURN_5H=$(_burn_ratio "$LIMIT_5H_USED_PCT" "$LIMIT_5H_RESETS_AT" "$now" "$WINDOW_5H")
        BURN_7D=$(_burn_ratio "$LIMIT_7D_USED_PCT" "$LIMIT_7D_RESETS_AT" "$now" "$WINDOW_7D")
    fi
}
