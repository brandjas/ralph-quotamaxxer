#!/usr/bin/env bash
# persist.sh — I/O layer: write state file and append history log.
# Sourced by statusline.sh. Expects DATA_DIR and parsed variables to be set.

_build_json() {
    local now_epoch now_iso hn
    now_epoch=$(date +%s)
    now_iso=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    hn=$(hostname)

    jq -n \
        --arg ts_iso "$now_iso" \
        --argjson ts_epoch "$now_epoch" \
        --arg hostname "$hn" \
        --arg session_id "$SESSION_ID" \
        --arg model_id "$MODEL_ID" \
        --arg model_name "$MODEL_NAME" \
        --argjson context_pct "$CONTEXT_PCT" \
        --argjson cost_usd "$COST_USD" \
        --argjson limit_5h_used "$LIMIT_5H_USED_PCT" \
        --argjson limit_5h_resets "$LIMIT_5H_RESETS_AT" \
        --argjson burn_5h "$BURN_5H" \
        --argjson limit_7d_used "$LIMIT_7D_USED_PCT" \
        --argjson limit_7d_resets "$LIMIT_7D_RESETS_AT" \
        --argjson burn_7d "$BURN_7D" \
        '{
            timestamp: { iso: $ts_iso, epoch: $ts_epoch },
            hostname: $hostname,
            session_id: $session_id,
            model: { id: $model_id, name: $model_name },
            context_pct: $context_pct,
            cost_usd: $cost_usd,
            rate_limits: {
                five_hour:  { used_pct: $limit_5h_used, resets_at: $limit_5h_resets, burn_ratio: $burn_5h },
                seven_day:  { used_pct: $limit_7d_used, resets_at: $limit_7d_resets, burn_ratio: $burn_7d }
            }
        }'
}

persist_usage() {
    local json
    json=$(_build_json)

    local histfile="$DATA_DIR/usage-history.jsonl"
    local lockfile="$DATA_DIR/usage-history.jsonl.lock"
    local compact
    compact=$(printf '%s' "$json" | jq -c .)

    # State file: atomic write (no lock needed — last writer wins is acceptable)
    printf '%s\n' "$json" > "$DATA_DIR/usage-guard.json.tmp" \
        && mv "$DATA_DIR/usage-guard.json.tmp" "$DATA_DIR/usage-guard.json"

    # History file: locked append + rotation
    if command -v flock &>/dev/null; then
        (
            if ! flock -w 1 200; then
                return 0  # skip this append rather than write unlocked
            fi
            printf '%s\n' "$compact" >> "$histfile"
            _maybe_rotate "$histfile"
        ) 200>"$lockfile"
    else
        printf '%s\n' "$compact" >> "$histfile"
        _maybe_rotate "$histfile"
    fi
}

_maybe_rotate() {
    local file="$1"
    local max_bytes="${USAGE_HISTORY_MAX_BYTES:-10485760}"
    local size

    # Portable file size: wc -c works on GNU and BSD
    size=$(wc -c < "$file" 2>/dev/null || echo 0)

    if (( size > max_bytes )); then
        local total
        total=$(wc -l < "$file")
        local keep=$(( total / 2 ))
        if (( keep < 1 )); then keep=1; fi
        tail -n "$keep" "$file" > "$file.tmp" && mv "$file.tmp" "$file"
    fi
}
