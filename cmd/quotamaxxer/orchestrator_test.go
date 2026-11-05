package main

import (
	"testing"
	"time"
)

// TestRunTimeoutEscalatesToSIGKILL checks that a child ignoring SIGTERM is
// killed after the grace period instead of hanging the orchestrator forever.
func TestRunTimeoutEscalatesToSIGKILL(t *testing.T) {
	orig := runTimeoutKillGrace
	runTimeoutKillGrace = 500 * time.Millisecond
	defer func() { runTimeoutKillGrace = orig }()

	type result struct {
		code    int
		elapsed time.Duration
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		code := runOrchestrator([]string{
			"--data-dir", t.TempDir(), "--claude-command", "bash", "--run-timeout", "300ms",
			"--", "-c", `trap "" TERM; sleep 30 & wait; sleep 30`,
		})
		done <- result{code, time.Since(start)}
	}()

	select {
	case r := <-done:
		if r.code != 124 {
			t.Errorf("exit code = %d, want 124", r.code)
		}
		if r.elapsed > 5*time.Second {
			t.Errorf("took %s, want roughly run-timeout + grace", r.elapsed)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("orchestrator did not return: child ignoring SIGTERM was never killed")
	}
}
