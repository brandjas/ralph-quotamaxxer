#!/usr/bin/env bash
# statusline.sh — Entry point invoked by Claude Code's statusLine mechanism.
# Sources parse.sh and persist.sh, then echoes a colored single-line statusline.
#
# Claude Code pipes a JSON object to stdin after each assistant message (debounced 300ms).
# Relevant fields:
#   model.display_name, model.id
#   session_id
#   context_window.used_percentage
#   cost.total_cost_usd
#   rate_limits.five_hour.used_percentage   (0–100, Pro/Max only, absent until first API response)
#   rate_limits.five_hour.resets_at         (unix epoch seconds)
#   rate_limits.seven_day.used_percentage   (0–100)
#   rate_limits.seven_day.resets_at         (unix epoch seconds)
#
# Burn ratio = used_pct / elapsed_pct, where elapsed_pct is the fraction of the
# window's total duration already passed (derivable from resets_at and known
# window durations: 5h = 18000s, 7d = 604800s). A ratio of 1.0 means exactly
# on pace to exhaust quota at reset; >1.0 means burning too fast.

set -euo pipefail

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
DATA_DIR="$SCRIPT_DIR/../data"
mkdir -p "$DATA_DIR"

source "$SCRIPT_DIR/parse.sh"
source "$SCRIPT_DIR/persist.sh"

# --- ANSI colors ---
RED=$'\033[0;31m'
YELLOW=$'\033[0;33m'
GREEN=$'\033[0;32m'
DIM=$'\033[2m'
RESET=$'\033[0m'

color_for_pct() {
    local pct="$1"
    if (( pct > 80 )); then
        printf '%s' "$RED"
    elif (( pct > 50 )); then
        printf '%s' "$YELLOW"
    else
        printf '%s' "$GREEN"
    fi
}

burn_indicators() {
    awk -v r5h="$1" -v r7d="$2" 'BEGIN {
        split("▲ ● ▼", sym)
        printf "%s\t%s", (r5h > 0.95 ? sym[1] : r5h < 0.8 ? sym[3] : sym[2]), \
                         (r7d > 0.95 ? sym[1] : r7d < 0.8 ? sym[3] : sym[2])
    }'
}

# --- Main ---
parse_stdin

cost=$(printf '%.2f' "$COST_USD")
line="${DIM}${MODEL_NAME}${RESET} | Ctx ${CONTEXT_PCT}% | \$${cost}"

if [[ "$HAS_RATE_LIMITS" == "true" ]]; then
    persist_usage

    c5h=$(color_for_pct "${LIMIT_5H_USED_PCT%.*}")
    c7d=$(color_for_pct "${LIMIT_7D_USED_PCT%.*}")
    IFS=$'\t' read -r b5h b7d <<< "$(burn_indicators "$BURN_5H" "$BURN_7D")"

    line+=" | ${c5h}5h: ${LIMIT_5H_USED_PCT}%${b5h}${RESET}"
    line+=" | ${c7d}7d: ${LIMIT_7D_USED_PCT}%${b7d}${RESET}"
fi

printf '%s\n' "$line"
