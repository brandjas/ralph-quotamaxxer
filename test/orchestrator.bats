#!/usr/bin/env bats
# orchestrator.bats — tests for the default orchestrator mode.
# Uses --claude-command to substitute mock commands instead of real claude.

load helpers

@test "orchestrator: propagates exit code 0" {
    run "$QM_BIN" --claude-command true --
    [ "$status" -eq 0 ]
}

@test "orchestrator: propagates non-zero exit code" {
    run "$QM_BIN" --claude-command false --
    [ "$status" -eq 1 ]
}

@test "orchestrator: propagates specific exit code" {
    run "$QM_BIN" --claude-command bash -- -c "exit 42"
    [ "$status" -eq 42 ]
}

@test "orchestrator: sets ANTHROPIC_BASE_URL for child" {
    run "$QM_BIN" --claude-command bash -- -c 'echo "$ANTHROPIC_BASE_URL"'
    [ "$status" -eq 0 ]
    [[ "$output" == http://127.0.0.1:* ]]
}

@test "orchestrator: --run-timeout kills child and exits 124" {
    run "$QM_BIN" --claude-command sleep --run-timeout 1s -- 60
    [ "$status" -eq 124 ]
}

@test "orchestrator: child finishes before --run-timeout exits normally" {
    run "$QM_BIN" --claude-command sleep --run-timeout 10s -- 0
    [ "$status" -eq 0 ]
}

@test "orchestrator: passes args after -- to child" {
    run "$QM_BIN" --claude-command echo -- hello world
    [ "$status" -eq 0 ]
    [[ "$output" == "hello world" ]]
}

@test "orchestrator: double -- passes literal -- to child" {
    # 'quotamaxxer --claude-command echo -- -- hello' should pass '-- hello' to echo.
    run "$QM_BIN" --claude-command echo -- -- hello
    [ "$status" -eq 0 ]
    [[ "$output" == "-- hello" ]]
}

@test "orchestrator: unknown flags pass through to child without --" {
    run "$QM_BIN" --claude-command echo -p hello
    [ "$status" -eq 0 ]
    [[ "$output" == "-p hello" ]]
}

@test "orchestrator: own flags then claude flags without --" {
    write_proxy_json 0.1 0.1
    run "$QM_BIN" --claude-command echo --threshold-5h 0.8 --data-dir "$DATA_DIR" --quiet -p hello
    [ "$status" -eq 0 ]
    [[ "$output" == "-p hello" ]]
}

@test "orchestrator: --threshold-5h without value exits 1" {
    run "$QM_BIN" --threshold-5h --
    [ "$status" -eq 1 ]
    [[ "$output" == *"invalid value"* ]]
}

@test "orchestrator: guard blocks then child runs" {
    # Low utilization — guard should pass immediately.
    write_proxy_json 0.1 0.1
    run "$QM_BIN" --claude-command true --threshold-5h 0.8 --data-dir "$DATA_DIR" --quiet --
    [ "$status" -eq 0 ]
}

@test "orchestrator: guard timeout exits 1 (child never starts)" {
    write_proxy_json 0.95 0.1
    run "$QM_BIN" --claude-command true --threshold-5h 0.5 --wait-timeout 1s --data-dir "$DATA_DIR" --quiet --
    [ "$status" -eq 1 ]
}

@test "orchestrator: interactive child can control the TTY" {
    # Under a pty, the child must be in the terminal's foreground process
    # group; otherwise tcsetattr (as done by stty, or claude entering raw
    # mode) raises SIGTTOU and the child is stopped forever.
    command -v script >/dev/null || skip "script(1) not available"
    run timeout 20 script -qec "$QM_BIN --claude-command stty -- sane" /dev/null </dev/null
    [ "$status" -eq 0 ]
}

@test "orchestrator: stdin from /dev/null is headless (child gets own process group)" {
    # /dev/null is a character device but not a TTY. Headless runs must put
    # the child in its own process group so --run-timeout kills its whole tree.
    [[ -r /proc/self/stat ]] || skip "needs /proc"
    run "$QM_BIN" --claude-command bash -- -c '
        read -r _ _ _ ppid pgrp _ < /proc/$$/stat
        read -r _ _ _ _ ppgrp _ < /proc/$ppid/stat
        echo "child=$pgrp parent=$ppgrp"
        [ "$pgrp" != "$ppgrp" ]' </dev/null
    echo "$output"
    [ "$status" -eq 0 ]
}

@test "orchestrator: child inherits stdin" {
    # Pipe input to child via stdin.
    run bash -c 'echo "from stdin" | "$1" --claude-command cat --' -- "$QM_BIN"
    [ "$status" -eq 0 ]
    [[ "$output" == "from stdin" ]]
}

@test "orchestrator: honors QUOTAMAXXER_DATA_DIR without --data-dir" {
    write_proxy_json 0.95 0.1
    run "$QM_BIN" --claude-command true --threshold-5h 0.5 --wait-timeout 1s --quiet --
    [ "$status" -eq 1 ]
    [[ "$output" == *"timeout"* ]]
}

@test "orchestrator: guard reads statusline data by default" {
    write_statusline_json 95 10 1.9 0.2
    run "$QM_BIN" --claude-command true --threshold-5h 0.5 --wait-timeout 1s --quiet --
    [ "$status" -eq 1 ]
    [[ "$output" == *"timeout"* ]]
}
