#!/usr/bin/env bash
# statusline.sh — Entry point invoked by Claude Code's statusLine mechanism.
# Sources parse.sh for display variables, pipes raw stdin to Go binary for persistence.
#
# Claude Code pipes a JSON object to stdin after each assistant message (debounced 300ms).
# The Go binary (quotamaxxer statusline-persist) handles parsing and persistence.
# This script only handles display formatting.

set -euo pipefail

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
QUOTAMAXXER_BIN="${QUOTAMAXXER_BIN:-$SCRIPT_DIR/../bin/quotamaxxer}"
export QUOTAMAXXER_DATA_DIR="${QUOTAMAXXER_DATA_DIR:-$SCRIPT_DIR/../data}"
mkdir -p "$QUOTAMAXXER_DATA_DIR"

source "$SCRIPT_DIR/parse.sh"

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
# Save raw stdin so both parse and persist can use it.
RAW_STDIN=$(cat)
parse_stdin <<< "$RAW_STDIN"

cost=$(printf '%.2f' "$COST_USD")
line="${DIM}${MODEL_NAME}${RESET} | Ctx ${CONTEXT_PCT}% | \$${cost}"

if [[ "$HAS_RATE_LIMITS" == "true" ]]; then
    "$QUOTAMAXXER_BIN" statusline-persist <<< "$RAW_STDIN"

    c5h=$(color_for_pct "${LIMIT_5H_USED_PCT%.*}")
    c7d=$(color_for_pct "${LIMIT_7D_USED_PCT%.*}")
    IFS=$'\t' read -r b5h b7d <<< "$(burn_indicators "$BURN_5H" "$BURN_7D")"

    line+=" | ${c5h}5h: ${LIMIT_5H_USED_PCT}%${b5h}${RESET}"
    line+=" | ${c7d}7d: ${LIMIT_7D_USED_PCT}%${b7d}${RESET}"
fi

printf '%s\n' "$line"
