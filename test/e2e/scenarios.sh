#!/usr/bin/env bash
# scenarios.sh — runs ONE end-to-end scenario inside a fresh container.
# Started by run.sh; see there for the tiers. Usage: scenarios.sh <scenario>
set -euo pipefail

SCENARIOS=(
    headless_proxy
    bg_direct
    bg_wrapper
    interactive_default
    persistent_headless
    persistent_bg
    persistent_interactive
    persistent_revive
    persistent_upgrade
    persistent_fail_open
)
if [[ "${1:-}" == --list ]]; then
    printf '%s\n' "${SCENARIOS[@]}"
    exit 0
fi

SCENARIO="${1:?usage: scenarios.sh <scenario>}"
TIER="${E2E_TIER:?}"
PROMPT='Reply with exactly: ok'
MODEL=haiku
PORT=28429                    # default persistent proxy port
FAKE_ADDR=127.0.0.1:9911

QM_HOME="$HOME/.claude/ralph-quotamaxxer"
DATA="$QM_HOME/data"
HISTORY="$DATA/usage-history.jsonl"
UPSTREAM_LOG="$HOME/upstream.log"
WORKDIR="$HOME/work"

# --- setup: what a user's machine looks like after install --------------------

mkdir -p "$QM_HOME/bin" "$WORKDIR" "$HOME/.claude"
cp /opt/quotamaxxer/quotamaxxer "$QM_HOME/bin/quotamaxxer"
cat > "$HOME/.claude/settings.json" <<JSON
{ "statusLine": { "type": "command", "command": "$QM_HOME/bin/quotamaxxer statusline" } }
JSON
# A fresh folder is untrusted, and `claude --bg` refuses to start in one.
cat > "$HOME/.claude.json" <<JSON
{ "hasCompletedOnboarding": true, "projects": { "$WORKDIR": { "hasTrustDialogAccepted": true } } }
JSON
cd "$WORKDIR"

if [[ "$TIER" == fake ]]; then
    /opt/quotamaxxer/fakeupstream -addr "$FAKE_ADDR" -log "$UPSTREAM_LOG" 2>/dev/null &
    export ANTHROPIC_BASE_URL="http://$FAKE_ADDR"
    export CLAUDE_CODE_OAUTH_TOKEN=fake-token
    WAIT=40
else
    WAIT=120
fi

# --- helpers ------------------------------------------------------------------

fail() {
    exec >&2 # also when called inside $(...)
    echo "FAIL: $*"
    excerpt() { echo "--- $1"; { cat "$2" 2>&1 || true; } | tail -n 12 | cut -c1-240; }
    excerpt usage-history.jsonl "$HISTORY"
    excerpt persistent-proxy.log "$DATA/persistent-proxy.log"
    excerpt "upstream log" "$UPSTREAM_LOG"
    echo "--- sessions"; claude agents --json --all 2>&1 | jq -c '.[] | {id, status, state}' 2>&1 || true
    echo "--- processes"; ps aux 2>&1 | grep -E 'claude|quotamaxxer' | grep -v grep | cut -c1-200 || true
    exit 1
}

set -E
trap 'fail "unexpected error on line $LINENO"' ERR

# wait_for <description> <command...>: poll until the command succeeds.
wait_for() {
    local what="$1" i; shift
    for ((i = 0; i < WAIT * 2; i++)); do
        if "$@" >/dev/null 2>&1; then return 0; fi
        sleep 0.5
    done
    fail "timed out after ${WAIT}s waiting for $what"
}

# count_records <source> [session-id-prefix]: matching history records.
count_records() {
    # grep, not jq: the file may be read while a record is being appended.
    { grep -F "\"source\":\"$1\"" "$HISTORY" 2>/dev/null || true; } \
        | { if [[ -n "${2:-}" ]]; then grep -cF "\"session_id\":\"$2" || true; else wc -l; fi; }
}
has_records()      { [[ "$(count_records "$1" "${2:-}")" -gt "${3:-0}" ]]; }
proxy_snapshot_ok() { jq -e '(.five_hour // .seven_day).utilization != null' "$DATA/usage-proxy.json"; }

# upstream_turns: model turns the fake upstream has served (fake tier only).
upstream_turns() {
    { grep -F '"path":"/v1/messages"' "$UPSTREAM_LOG" 2>/dev/null || true; } | { grep -cF '"stream":true' || true; }
}

# progress: a number that grows with every completed model turn, whichever
# way the session is tracked. Statusline and proxy each record every turn.
progress() { echo $(( $(count_records statusline) + $(count_records proxy) + $(upstream_turns) )); }
progressed() { [[ "$(progress)" -gt "$1" ]]; }

# start_bg <command...>: start a background session, print its short id.
start_bg() {
    local out id
    out="$("$@" 2>&1)" || fail "'$*' failed: $out"
    id="$(sed -n 's/^backgrounded · \([0-9a-f]\{8\}\).*/\1/p' <<<"$out" | head -1)"
    [[ -n "$id" ]] || fail "no session id in output of '$*': $out"
    echo "$id"
}

# session_field <id> <field>: a field of session <id> in `claude agents --json`.
session_field() {
    { claude agents --json --all 2>/dev/null || true; } | { jq -r --arg id "$1" --arg f "$2" \
        '.[] | select((.sessionId // "") | startswith($id)) | .[$f]' 2>/dev/null || true; } | head -1
}
# The state is "stopped", or "done" if the model declared the task finished.
session_gone() { [[ ! -d "/proc/$2" && "$(session_field "$1" state)" =~ ^(stopped|done)$ ]]; }
session_idle() { [[ "$(session_field "$1" status)" == idle ]]; }

# turn <id> <command...>: run another turn in session <id> and wait until it
# is recorded. Resuming a session that is still running would start a copy,
# so the session is stopped first and then continued under its own id, which
# takes the full id.
turn() {
    local id="$1" full pid before out; shift
    wait_for "session $id to go idle" session_idle "$id"
    full="$(session_field "$id" sessionId)"
    pid="$(session_field "$id" pid)"
    [[ "$pid" =~ ^[0-9]+$ ]] || fail "no pid for session $id"
    claude stop "$id" >/dev/null 2>&1 || fail "could not stop session $id"
    # `claude stop` returns before the session's process is gone.
    wait_for "session $id (pid $pid) to exit" session_gone "$id" "$pid"
    before="$(progress)"
    out="$("$@" --bg --resume "$full" "$PROMPT" 2>&1)" || fail "resume of $id failed: $out"
    grep -q "backgrounded · $id" <<<"$out" || fail "resume did not continue session $id: $out"
    wait_for "the resumed turn of $id to be recorded" progressed "$before"
}

# no_connection_errors <id>: the session never failed to reach the API.
no_connection_errors() {
    local logs
    logs="$(timeout 15 claude logs "$1" 2>&1 || true)"
    if grep -qiE 'ECONNREFUSED|Unable to connect|Connection error' <<<"$logs"; then
        fail "session $1 hit a connection error"
    fi
}

# start_interactive <command...>: run an interactive session on a pty, in the
# background. stop_interactive ends it.
start_interactive() {
    ( sleep 100000 | script -qfec "$*" /dev/null > "$HOME/tty.log" 2>&1 ) &
    INTERACTIVE_JOB=$!
}
stop_interactive() {
    pkill -f "^script -qfec" 2>/dev/null || true
    pkill -f "sleep 100000" 2>/dev/null || true
    wait "$INTERACTIVE_JOB" 2>/dev/null || true
}

daemon_pid() { quotamaxxer daemon status | sed -n 's/.*pid \([0-9]*\).*/\1/p'; }
daemon_running() { quotamaxxer daemon status >/dev/null 2>&1; }

# --- default mode -------------------------------------------------------------

# A headless run is recorded by the in-process proxy, which leaves nothing behind.
headless_proxy() {
    local out
    out="$(quotamaxxer -p --model "$MODEL" "$PROMPT" </dev/null)" || fail "headless run failed: $out"
    grep -qi 'ok' <<<"$out" || fail "unexpected reply: $out"
    proxy_snapshot_ok >/dev/null || fail "no usable usage-proxy.json"
    if pgrep -f 'quotamaxxer daemon' >/dev/null; then fail "default mode left a daemon running"; fi
}

# A background agent started with nothing else running is recorded by the
# statusline, and so is a later turn. $1 is how claude is launched.
bg_default() {
    local id
    id="$(start_bg "$@" --bg --model "$MODEL" "$PROMPT")"
    wait_for "a statusline record for $id" has_records statusline "$id"
    turn "$id" "$@"
    no_connection_errors "$id"
    [[ ! -e "$DATA/usage-proxy.json" ]] || fail "a background session wrote usage-proxy.json in default mode"
}
bg_direct()  { bg_default claude; }
bg_wrapper() { bg_default quotamaxxer --; }

# An interactive session is recorded by the statusline, not proxied.
interactive_default() {
    start_interactive "quotamaxxer --model $MODEL '$PROMPT'"
    wait_for "a statusline record" has_records statusline
    stop_interactive
    [[ ! -e "$DATA/usage-proxy.json" ]] || fail "an interactive session wrote usage-proxy.json in default mode"
    if pgrep -f 'quotamaxxer daemon' >/dev/null; then fail "default mode left a daemon running"; fi
}

# --- persistent proxy (opt-in) ------------------------------------------------

# A headless run goes through the daemon, which stays up afterwards.
persistent_headless() {
    export QUOTAMAXXER_PERSISTENT=1
    local out
    out="$(quotamaxxer -p --model "$MODEL" "$PROMPT" </dev/null)" || fail "headless run failed: $out"
    grep -qi 'ok' <<<"$out" || fail "unexpected reply: $out"
    proxy_snapshot_ok >/dev/null || fail "no usable usage-proxy.json"
    daemon_running || fail "the daemon is not running after the launch"
}

# A background agent is proxied, and keeps working after its launcher is gone:
# for a later turn started without quotamaxxer, and after being attached to.
persistent_bg() {
    export QUOTAMAXXER_PERSISTENT=1
    local id
    id="$(start_bg quotamaxxer -- --bg --model "$MODEL" "$PROMPT")"
    wait_for "a proxy record" has_records proxy
    wait_for "a statusline record for $id" has_records statusline "$id"

    local before; before="$(count_records proxy)"
    turn "$id" quotamaxxer --
    wait_for "the second turn to go through the proxy" has_records proxy "" "$before"

    start_interactive "claude attach $id"
    sleep 3
    stop_interactive
    before="$(count_records proxy)"
    turn "$id" quotamaxxer --
    wait_for "the turn after attaching to go through the proxy" has_records proxy "" "$before"
    no_connection_errors "$id"
}

# An interactive session is proxied, and the daemon outlives it.
persistent_interactive() {
    start_interactive "QUOTAMAXXER_PERSISTENT=1 quotamaxxer --model $MODEL '$PROMPT'"
    wait_for "a proxy record" has_records proxy
    stop_interactive
    daemon_running || fail "the daemon did not outlive the interactive session"
}

# If the daemon dies, a session that depends on it brings it back by itself.
persistent_revive() {
    export QUOTAMAXXER_PERSISTENT=1
    local id
    id="$(start_bg quotamaxxer -- --bg --model "$MODEL" "$PROMPT")"
    wait_for "a proxy record" has_records proxy
    wait_for "a statusline record for $id" has_records statusline "$id"

    kill -9 "$(daemon_pid)"
    local before; before="$(count_records proxy)"
    turn "$id" quotamaxxer --
    wait_for "the turn after the daemon died to go through the proxy" has_records proxy "" "$before"
    daemon_running || fail "the daemon was not revived"
}

# Installing a new build replaces the running daemon on the next launch,
# without breaking a session that was started through the old one.
persistent_upgrade() {
    export QUOTAMAXXER_PERSISTENT=1
    local id old_pid new_pid out
    id="$(start_bg quotamaxxer -- --bg --model "$MODEL" "$PROMPT")"
    wait_for "a proxy record" has_records proxy
    old_pid="$(daemon_pid)"

    # "Upgrade": same program, different bytes, moved into place.
    cp "$QM_HOME/bin/quotamaxxer" "$QM_HOME/bin/quotamaxxer.new"
    echo >> "$QM_HOME/bin/quotamaxxer.new"
    mv "$QM_HOME/bin/quotamaxxer.new" "$QM_HOME/bin/quotamaxxer"

    out="$(quotamaxxer -p --model "$MODEL" "$PROMPT" </dev/null)" || fail "headless run after upgrade failed: $out"
    new_pid="$(daemon_pid)"
    [[ -n "$new_pid" && "$new_pid" != "$old_pid" ]] || fail "the daemon was not replaced (pid $old_pid -> $new_pid)"
    if quotamaxxer daemon status | grep -q 'different build'; then fail "the new daemon is not the new build"; fi

    local before; before="$(count_records proxy)"
    turn "$id" quotamaxxer --
    wait_for "the old session's turn to go through the new daemon" has_records proxy "" "$before"
}

# If the port is taken by something else, quotamaxxer says so and behaves
# exactly as it does by default.
persistent_fail_open() {
    export QUOTAMAXXER_PERSISTENT=1
    /opt/quotamaxxer/fakeupstream -addr "127.0.0.1:$PORT" 2>/dev/null &
    wait_for "the port squatter" curl -s -o /dev/null "http://127.0.0.1:$PORT/"

    local out id
    out="$(quotamaxxer -p --model "$MODEL" "$PROMPT" </dev/null 2>"$HOME/stderr.log")" || fail "headless run failed: $out"
    grep -qi 'ok' <<<"$out" || fail "unexpected reply: $out"
    grep -q 'persistent proxy unavailable' "$HOME/stderr.log" || fail "no fallback warning: $(cat "$HOME/stderr.log")"
    proxy_snapshot_ok >/dev/null || fail "the fallback headless run was not recorded"

    id="$(start_bg quotamaxxer -- --bg --model "$MODEL" "$PROMPT")"
    wait_for "a statusline record for $id" has_records statusline "$id"
}

# --- run ----------------------------------------------------------------------

case " ${SCENARIOS[*]} " in
    *" $SCENARIO "*) ;;
    *) echo "unknown scenario: $SCENARIO" >&2; exit 2 ;;
esac
"$SCENARIO"
echo "ok: $SCENARIO ($TIER)"
