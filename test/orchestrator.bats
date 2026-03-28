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

@test "orchestrator: unknown flag before -- exits 1" {
    run "$QM_BIN" --bogus-flag --
    [ "$status" -eq 1 ]
    [[ "$output" == *"not defined"* ]]
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

@test "orchestrator: child inherits stdin" {
    # Pipe input to child via stdin.
    run bash -c 'echo "from stdin" | "$1" --claude-command cat --' -- "$QM_BIN"
    [ "$status" -eq 0 ]
    [[ "$output" == "from stdin" ]]
}
