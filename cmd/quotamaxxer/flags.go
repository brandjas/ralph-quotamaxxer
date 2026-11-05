package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
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

// parseOrchestratorArgs parses leading quotamaxxer flags from args. Parsing
// stops at the first argument that isn't a quotamaxxer flag (or its value);
// that argument and everything after it become ClaudeArgs verbatim. An
// explicit "--" also ends quotamaxxer flags and is itself dropped, so
// "quotamaxxer -- -- x" passes "-- x" to claude.
func parseOrchestratorArgs(args []string) (orchestratorConfig, error) {
	fs := flag.NewFlagSet("quotamaxxer", flag.ContinueOnError)

	cfg := orchestratorConfig{}
	fs.Float64Var(&cfg.Threshold5h, "threshold-5h", 0, "")
	fs.Float64Var(&cfg.Threshold7d, "threshold-7d", 0, "")
	fs.DurationVar(&cfg.WaitTimeout, "wait-timeout", 0, "")
	fs.DurationVar(&cfg.RunTimeout, "run-timeout", 0, "")
	fs.StringVar(&cfg.Source, "source", "proxy", "")
	fs.BoolVar(&cfg.Quiet, "quiet", false, "")
	fs.StringVar(&cfg.DataDir, "data-dir", resolveDefaultDataDir(), "")
	fs.StringVar(&cfg.ClaudeCmd, "claude-command", "claude", "")

	help := false
	fs.BoolVar(&help, "help", false, "")
	fs.BoolVar(&help, "h", false, "")

	// Errors are returned (and printed by the caller); we have our own help text.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	split := splitOwnFlags(fs, args)
	if err := fs.Parse(args[:split]); err != nil {
		return cfg, fmt.Errorf("%v\nRun 'quotamaxxer --help' for usage.", err)
	}

	if help {
		runHelp()
		os.Exit(0)
	}

	if err := validateSource(cfg.Source); err != nil {
		return cfg, err
	}

	rest := args[split:]
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	cfg.ClaudeArgs = rest

	return cfg, nil
}

// splitOwnFlags returns the index of the first argument that does not belong
// to a flag defined in fs. Arguments before that index are quotamaxxer flags
// (and their values); the argument at that index is either "--" or the first
// argument destined for claude.
func splitOwnFlags(fs *flag.FlagSet, args []string) int {
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--" || len(arg) < 2 || arg[0] != '-' {
			return i
		}
		name := strings.TrimLeft(arg, "-")
		hasValue := false
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name, hasValue = name[:eq], true
		}
		f := fs.Lookup(name)
		if f == nil {
			return i
		}
		i++
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			continue
		}
		if !hasValue && i < len(args) {
			i++ // consume the flag's value
		}
	}
	return i
}

func validateSource(source string) error {
	switch source {
	case "both", "proxy", "statusline":
		return nil
	default:
		return fmt.Errorf("invalid --source %q (must be both, proxy, or statusline)", source)
	}
}
