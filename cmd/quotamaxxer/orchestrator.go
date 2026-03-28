package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// runOrchestrator is the default mode: guard → proxy → claude.
func runOrchestrator(args []string) int {
	cfg, err := parseOrchestratorArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "quotamaxxer:", err)
		return 1
	}

	dataDir := cfg.DataDir
	upstream := envOr("QUOTAMAXXER_UPSTREAM", defaultUpstream)

	// Run guard if thresholds are set.
	if cfg.Threshold5h > 0 || cfg.Threshold7d > 0 {
		gcfg := guardConfig{
			Threshold5h: cfg.Threshold5h,
			Threshold7d: cfg.Threshold7d,
			DataDir:     dataDir,
			WaitTimeout: cfg.WaitTimeout,
			Quiet:       cfg.Quiet,
			Source:      cfg.Source,
		}
		if err := guardLoop(gcfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	// Start proxy in-process.
	addr, shutdownProxy, err := startProxyServer(dataDir, upstream, "0")
	if err != nil {
		log.Printf("quotamaxxer: failed to start proxy: %v", err)
		return 1
	}
	defer shutdownProxy()

	// Launch claude (or custom command via --claude-command).
	cmd := exec.Command(cfg.ClaudeCmd, cfg.ClaudeArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "ANTHROPIC_BASE_URL=http://"+addr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer: failed to start claude: %v\n", err)
		return 1
	}

	// Run timeout.
	var timedOut atomic.Bool
	if cfg.RunTimeout > 0 {
		timer := time.AfterFunc(cfg.RunTimeout, func() {
			timedOut.Store(true)
			fmt.Fprintf(os.Stderr, "quotamaxxer: --run-timeout (%s) exceeded, killing claude\n", cfg.RunTimeout)
			syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		})
		defer timer.Stop()
	}

	// Forward signals to claude's process group.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for sig := range sigCh {
			syscall.Kill(-cmd.Process.Pid, sig.(syscall.Signal))
		}
	}()

	// Wait for claude to exit.
	err = cmd.Wait()
	signal.Stop(sigCh)

	if timedOut.Load() {
		return 124
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	if err != nil {
		return 1
	}
	return 0
}
