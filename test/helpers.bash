# helpers.bash — shared setup/teardown for bats tests.

# Build binary once per test file (cached in BATS_FILE_TMPDIR).
QM_BIN="${BATS_FILE_TMPDIR}/quotamaxxer"

setup_file() {
    (cd "$BATS_TEST_DIRNAME/../cmd/quotamaxxer" && go build -o "$QM_BIN" .)
}

setup() {
    DATA_DIR="$(mktemp -d)"
    export QUOTAMAXXER_DATA_DIR="$DATA_DIR"

    NOW=$(date +%s)
    FIVE_HOUR_RESET=$(( NOW + 9000 ))   # halfway through 5h window
    SEVEN_DAY_RESET=$(( NOW + 302400 )) # halfway through 7d window
}

teardown() {
    rm -rf "$DATA_DIR"
}

# write_proxy_json <5h_util> <7d_util> [epoch]
write_proxy_json() {
    local util5h="${1}" util7d="${2}" epoch="${3:-$NOW}"
    cat > "$DATA_DIR/usage-proxy.json" <<EOF
{
  "source": "proxy",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "epoch": ${epoch},
  "status": "allowed",
  "five_hour": { "utilization": ${util5h}, "reset": ${FIVE_HOUR_RESET} },
  "seven_day": { "utilization": ${util7d}, "reset": ${SEVEN_DAY_RESET} }
}
EOF
}

# write_statusline_json <5h_pct> <7d_pct> <5h_burn> <7d_burn> [epoch]
write_statusline_json() {
    local pct5h="${1}" pct7d="${2}" burn5h="${3}" burn7d="${4}" epoch="${5:-$NOW}"
    cat > "$DATA_DIR/usage-statusline.json" <<EOF
{
  "source": "statusline",
  "timestamp": { "iso": "$(date -u +%Y-%m-%dT%H:%M:%SZ)", "epoch": ${epoch} },
  "rate_limits": {
    "five_hour": { "used_pct": ${pct5h}, "resets_at": ${FIVE_HOUR_RESET}, "burn_ratio": ${burn5h} },
    "seven_day": { "used_pct": ${pct7d}, "resets_at": ${SEVEN_DAY_RESET}, "burn_ratio": ${burn7d} }
  }
}
EOF
}
