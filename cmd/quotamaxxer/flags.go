package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

type orchestratorConfig struct {
	Threshold5h float64
	Threshold7d float64
	WaitTimeout time.Duration
	RunTimeout  time.Duration
	Source      string
	Quiet       bool
	DataDir     string
	ClaudeCmd   string
	ClaudeArgs  []string
}

// parseOrchestratorArgs parses quotamaxxer flags from args. Everything after
// "--" (handled by flag.FlagSet) becomes ClaudeArgs. If no flags are
// recognized, all args pass through to claude.
func parseOrchestratorArgs(args []string) (orchestratorConfig, error) {
	fs := flag.NewFlagSet("quotamaxxer", flag.ContinueOnError)

	cfg := orchestratorConfig{}
	fs.Float64Var(&cfg.Threshold5h, "threshold-5h", 0, "")
	fs.Float64Var(&cfg.Threshold7d, "threshold-7d", 0, "")
	fs.DurationVar(&cfg.WaitTimeout, "wait-timeout", 0, "")
	fs.DurationVar(&cfg.RunTimeout, "run-timeout", 0, "")
	fs.StringVar(&cfg.Source, "source", "both", "")
	fs.BoolVar(&cfg.Quiet, "quiet", false, "")
	fs.StringVar(&cfg.DataDir, "data-dir", resolveDefaultDataDir(), "")
	fs.StringVar(&cfg.ClaudeCmd, "claude-command", "claude", "")

	help := false
	fs.BoolVar(&help, "help", false, "")
	fs.BoolVar(&help, "h", false, "")

	// Suppress default usage — we have our own help text.
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return cfg, fmt.Errorf("%v\nRun 'quotamaxxer --help' for usage.", err)
	}

	if help {
		runHelp()
		os.Exit(0)
	}

	// Validate --source.
	switch cfg.Source {
	case "both", "proxy", "statusline":
	default:
		return cfg, fmt.Errorf("invalid --source %q (must be both, proxy, or statusline)", cfg.Source)
	}

	// Everything after "--" becomes claude args.
	cfg.ClaudeArgs = fs.Args()

	return cfg, nil
}
