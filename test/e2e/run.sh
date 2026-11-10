#!/usr/bin/env bash
# run.sh — end-to-end tests: real Claude Code + quotamaxxer in containers.
#
# Each scenario (see scenarios.sh) runs in a fresh container, on a musl image
# and on new and old glibc images, in up to two tiers:
#
#   fake  Claude Code talks to a canned stand-in for the API. No credentials,
#         no network (--network none), no cost. Always runs.
#   live  Claude Code talks to the real API with tiny Haiku prompts. Needs a
#         long-lived token from `claude setup-token`, in $CLAUDE_CODE_OAUTH_TOKEN
#         or in ~/.config/quotamaxxer/e2e-token; skipped when there is none.
#
# The containers never see the host's ~/.claude or its login credentials.
#
# Usage: test/e2e/run.sh [options]
#   --tier fake|live|all      Tiers to run (default: all)
#   --image <base>            Base image; repeatable (default: alpine:latest,
#                             debian:trixie-slim, ubuntu:20.04)
#   --scenario <name>         Scenario; repeatable (default: all)
#   --claude-version <v>      Claude Code version to install (default: latest)
#   --rebuild                 Rebuild images without cache (new Claude Code)
#   --list                    List scenarios and exit
set -euo pipefail

E2E_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$E2E_DIR/../.." && pwd)"

TIER=all
IMAGES=()
SCENARIOS=()
CLAUDE_VERSION=latest
REBUILD=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --tier)           TIER="$2"; shift 2 ;;
        --image)          IMAGES+=("$2"); shift 2 ;;
        --scenario)       SCENARIOS+=("$2"); shift 2 ;;
        --claude-version) CLAUDE_VERSION="$2"; shift 2 ;;
        --rebuild)        REBUILD=(--no-cache --pull); shift ;;
        --list)           bash "$E2E_DIR/scenarios.sh" --list; exit 0 ;;
        -h|--help)        sed -n '2,/^set -euo/p' "$0" | sed '$d; s/^# \{0,1\}//'; exit 0 ;;
        *)                echo "unknown option: $1" >&2; exit 2 ;;
    esac
done
[[ ${#IMAGES[@]} -gt 0 ]] || IMAGES=(alpine:latest debian:trixie-slim ubuntu:20.04)
if [[ ${#SCENARIOS[@]} -eq 0 ]]; then
    mapfile -t SCENARIOS < <(bash "$E2E_DIR/scenarios.sh" --list)
fi

case "$TIER" in
    fake) TIERS=(fake) ;;
    live) TIERS=(live) ;;
    all)  TIERS=(fake live) ;;
    *)    echo "invalid --tier: $TIER" >&2; exit 2 ;;
esac

# Live tier credentials. Only ever handed to containers as an env var.
TOKEN_FILE="${QUOTAMAXXER_E2E_TOKEN_FILE:-$HOME/.config/quotamaxxer/e2e-token}"
if [[ -z "${CLAUDE_CODE_OAUTH_TOKEN:-}" && -r "$TOKEN_FILE" ]]; then
    CLAUDE_CODE_OAUTH_TOKEN="$(tr -d '[:space:]' < "$TOKEN_FILE")"
fi
export CLAUDE_CODE_OAUTH_TOKEN="${CLAUDE_CODE_OAUTH_TOKEN:-}"
LIVE_SKIPPED=0
if [[ -z "$CLAUDE_CODE_OAUTH_TOKEN" && " ${TIERS[*]} " == *" live "* ]]; then
    echo "SKIP live tier: no token in \$CLAUDE_CODE_OAUTH_TOKEN or $TOKEN_FILE (create one with 'claude setup-token')"
    LIVE_SKIPPED=1
    if [[ "$TIER" == live ]]; then exit 0; fi
    TIERS=(fake)
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin" "$WORK/results"

# Linux binaries for the containers, whatever the host is. The container
# user is not the host user, so everything mounted must be world-readable.
echo "Building static binaries..."
(cd "$REPO_DIR/cmd/quotamaxxer" \
    && CGO_ENABLED=0 GOOS=linux go build -o "$WORK/bin/quotamaxxer" . \
    && CGO_ENABLED=0 GOOS=linux go build -o "$WORK/bin/fakeupstream" ./internal/fakeupstream)
chmod 755 "$WORK" "$WORK/bin" "$WORK/bin"/*

image_tag() { echo "quotamaxxer-e2e:$(echo "$1" | tr -c 'a-zA-Z0-9\n' '-')"; }

for base in "${IMAGES[@]}"; do
    echo "Building image for $base..."
    docker build -q "${REBUILD[@]}" \
        --build-arg BASE="$base" --build-arg CLAUDE_VERSION="$CLAUDE_VERSION" \
        -t "$(image_tag "$base")" "$E2E_DIR" >/dev/null
done

# run_image <base>: every tier and scenario for one image, sequentially.
run_image() {
    local base="$1" tag tier scenario log status
    tag="$(image_tag "$base")"
    for tier in "${TIERS[@]}"; do
        for scenario in "${SCENARIOS[@]}"; do
            log="$WORK/results/$(echo "$base" | tr -c 'a-zA-Z0-9\n' '-').$tier.$scenario.log"
            local net=()
            local env=(-e E2E_TIER="$tier")
            if [[ "$tier" == fake ]]; then
                net=(--network none)
            else
                env+=(-e CLAUDE_CODE_OAUTH_TOKEN)
            fi
            status=PASS
            docker run --rm --init "${net[@]}" "${env[@]}" \
                -v "$WORK/bin:/opt/quotamaxxer:ro" \
                -v "$E2E_DIR/scenarios.sh:/opt/scenarios.sh:ro" \
                "$tag" bash /opt/scenarios.sh "$scenario" >"$log" 2>&1 || status=FAIL
            printf '%s  %-20s %-5s %s\n' "$status" "$base" "$tier" "$scenario"
            if [[ "$status" == FAIL ]]; then
                sed "s/^/    | /" "$log"
                echo "$base $tier $scenario" >> "$WORK/results/failures"
            fi
        done
    done
}

echo "Running ${#SCENARIOS[@]} scenario(s) on ${#IMAGES[@]} image(s)..."
for base in "${IMAGES[@]}"; do
    run_image "$base" &
done
wait

if [[ -s "$WORK/results/failures" ]]; then
    echo
    echo "FAILED:"
    sed 's/^/  /' "$WORK/results/failures"
    exit 1
fi
echo
if [[ "$LIVE_SKIPPED" == 1 ]]; then
    echo "All fake-tier scenarios passed (live tier skipped: no token)."
else
    echo "All scenarios passed."
fi
