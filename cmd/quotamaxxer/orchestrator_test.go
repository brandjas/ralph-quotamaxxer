package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
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

// TestHeadlessProxyForwardsToInheritedBaseURL checks that a headless run's
// proxy forwards to the ANTHROPIC_BASE_URL quotamaxxer was launched with
// (e.g. a gateway) rather than silently going to api.anthropic.com.
func TestHeadlessProxyForwardsToInheritedBaseURL(t *testing.T) {
	var hits atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.33")
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Reset", "1774540800")
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	t.Setenv("ANTHROPIC_BASE_URL", gateway.URL)
	t.Setenv("QUOTAMAXXER_UPSTREAM", "")
	dataDir := t.TempDir()

	code := runOrchestrator([]string{
		"--data-dir", dataDir, "--claude-command", "bash", "--",
		"-c", `curl -s -o /dev/null --max-time 5 -X POST "$ANTHROPIC_BASE_URL/v1/messages" -d '{}'`,
		"-p",
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if hits.Load() == 0 {
		t.Fatal("gateway received no requests: proxy did not forward to the inherited ANTHROPIC_BASE_URL")
	}
	snap, err := readProxyFile(filepath.Join(dataDir, "usage-proxy.json"))
	if err != nil {
		t.Fatalf("no proxy snapshot: %v", err)
	}
	if snap.FiveHourUtil != 0.33 {
		t.Errorf("5h utilization = %v, want 0.33", snap.FiveHourUtil)
	}
}

func TestOrchestratorUpstreamPrecedence(t *testing.T) {
	t.Setenv("QUOTAMAXXER_UPSTREAM", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	if got := orchestratorUpstream(); got != defaultUpstream {
		t.Errorf("default = %q, want %q", got, defaultUpstream)
	}
	t.Setenv("ANTHROPIC_BASE_URL", "https://gateway.example")
	if got := orchestratorUpstream(); got != "https://gateway.example" {
		t.Errorf("with ANTHROPIC_BASE_URL = %q", got)
	}
	t.Setenv("QUOTAMAXXER_UPSTREAM", "https://explicit.example")
	if got := orchestratorUpstream(); got != "https://explicit.example" {
		t.Errorf("QUOTAMAXXER_UPSTREAM should win, got %q", got)
	}
}
