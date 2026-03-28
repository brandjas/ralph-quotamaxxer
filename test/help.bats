#!/usr/bin/env bats
# help.bats — tests for help output and flag validation.

load helpers

@test "help: 'help' subcommand shows usage" {
    run "$QM_BIN" help
    [ "$status" -eq 0 ]
    [[ "$output" == *"quotamaxxer"* ]]
    [[ "$output" == *"Usage:"* ]]
}

@test "help: --help flag shows usage" {
    run "$QM_BIN" --help
    [ "$status" -eq 0 ]
    [[ "$output" == *"Usage:"* ]]
}

@test "help: -h flag shows usage" {
    run "$QM_BIN" -h
    [ "$status" -eq 0 ]
    [[ "$output" == *"Usage:"* ]]
}

@test "help: documents --claude-command" {
    run "$QM_BIN" help
    [[ "$output" == *"--claude-command"* ]]
}

@test "help: documents --run-timeout" {
    run "$QM_BIN" help
    [[ "$output" == *"--run-timeout"* ]]
}

@test "help: documents guard subcommand" {
    run "$QM_BIN" help
    [[ "$output" == *"guard"* ]]
}

@test "help: documents proxy subcommand" {
    run "$QM_BIN" help
    [[ "$output" == *"proxy"* ]]
}

@test "help: documents statusline subcommand" {
    run "$QM_BIN" help
    [[ "$output" == *"statusline"* ]]
}
