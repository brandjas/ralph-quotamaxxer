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

// runTimeoutKillGrace is how long claude gets to exit after --run-timeout
// sends SIGTERM before it is sent SIGKILL. A var so tests can shorten it.
var runTimeoutKillGrace = 10 * time.Second

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

	// Launch claude (or custom command via --claude-command).
	cmd := exec.Command(cfg.ClaudeCmd, cfg.ClaudeArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	// Headless runs record usage through the in-process proxy. UI sessions
	// (interactive, --bg) rely on the statusline instead; see needsProxy.
	if needsProxy(cfg.ClaudeArgs, isTerminal(), isStdoutTerminal()) {
		addr, shutdownProxy, err := startProxyServer(dataDir, upstream, "0")
		if err != nil {
			log.Printf("quotamaxxer: failed to start proxy: %v", err)
			return 1
		}
		defer shutdownProxy()
		cmd.Env = append(cmd.Env, "ANTHROPIC_BASE_URL=http://"+addr)
	}

	// Interactive: claude must stay in our process group, which is the
	// terminal's foreground group. In its own group it would be stopped by
	// SIGTTIN/SIGTTOU as soon as it touched the TTY. Being in our group also
	// means terminal-generated signals (Ctrl-C, Ctrl-\, Ctrl-Z) reach it
	// directly, so we must not forward those or claude would see them twice.
	//
	// Headless: put claude in its own process group so --run-timeout and
	// forwarded signals reach its whole process tree (tools, MCP servers).
	interactive := isTerminal()
	if !interactive {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer: failed to start claude: %v\n", err)
		return 1
	}

	signalChild := func(sig syscall.Signal) {
		if interactive {
			syscall.Kill(cmd.Process.Pid, sig)
		} else {
			syscall.Kill(-cmd.Process.Pid, sig)
		}
	}

	// Run timeout: SIGTERM, then SIGKILL if claude (or anything in its
	// process group) is still running after the grace period.
	var timedOut atomic.Bool
	exited := make(chan struct{})
	if cfg.RunTimeout > 0 {
		grace := runTimeoutKillGrace // read once; the timer may outlive this call
		timer := time.AfterFunc(cfg.RunTimeout, func() {
			timedOut.Store(true)
			fmt.Fprintf(os.Stderr, "quotamaxxer: --run-timeout (%s) exceeded, killing claude\n", cfg.RunTimeout)
			signalChild(syscall.SIGTERM)
			select {
			case <-exited:
			case <-time.After(grace):
				fmt.Fprintf(os.Stderr, "quotamaxxer: claude still running %s after SIGTERM, sending SIGKILL\n", grace)
				signalChild(syscall.SIGKILL)
			}
		})
		defer timer.Stop()
	}

	// Catch signals so quotamaxxer outlives claude (and can report its exit
	// code), forwarding the ones claude wouldn't otherwise receive.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		for sig := range sigCh {
			s := sig.(syscall.Signal)
			if interactive && (s == syscall.SIGINT || s == syscall.SIGQUIT) {
				continue // already delivered to claude by the terminal
			}
			signalChild(s)
		}
	}()

	// Wait for claude to exit.
	err = cmd.Wait()
	close(exited)
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
