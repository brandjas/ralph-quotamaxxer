package main

import (
	"fmt"
	"os"
	"strconv"
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
	ClaudeArgs  []string
}

// parseOrchestratorArgs splits args on the first "--" and parses quotamaxxer
// flags from the left side. Everything after "--" becomes ClaudeArgs.
// If no "--" is found, all args become ClaudeArgs (no quotamaxxer flags).
func parseOrchestratorArgs(args []string) (orchestratorConfig, error) {
	cfg := orchestratorConfig{
		Source:  "both",
		DataDir: resolveDefaultDataDir(),
	}

	// Find the first "--".
	sepIdx := -1
	for i, a := range args {
		if a == "--" {
			sepIdx = i
			break
		}
	}

	if sepIdx < 0 {
		// No separator — all args go to claude.
		cfg.ClaudeArgs = args
		return cfg, nil
	}

	cfg.ClaudeArgs = args[sepIdx+1:]
	qmArgs := args[:sepIdx]

	i := 0
	for i < len(qmArgs) {
		switch qmArgs[i] {
		case "--threshold-5h":
			v, err := requireArg(qmArgs, &i, "--threshold-5h")
			if err != nil {
				return cfg, err
			}
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return cfg, fmt.Errorf("invalid --threshold-5h value %q", v)
			}
			cfg.Threshold5h = f
		case "--threshold-7d":
			v, err := requireArg(qmArgs, &i, "--threshold-7d")
			if err != nil {
				return cfg, err
			}
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return cfg, fmt.Errorf("invalid --threshold-7d value %q", v)
			}
			cfg.Threshold7d = f
		case "--wait-timeout":
			v, err := requireArg(qmArgs, &i, "--wait-timeout")
			if err != nil {
				return cfg, err
			}
			d, err := time.ParseDuration(v)
			if err != nil {
				return cfg, fmt.Errorf("invalid --wait-timeout value %q", v)
			}
			cfg.WaitTimeout = d
		case "--run-timeout":
			v, err := requireArg(qmArgs, &i, "--run-timeout")
			if err != nil {
				return cfg, err
			}
			d, err := time.ParseDuration(v)
			if err != nil {
				return cfg, fmt.Errorf("invalid --run-timeout value %q", v)
			}
			cfg.RunTimeout = d
		case "--source":
			v, err := requireArg(qmArgs, &i, "--source")
			if err != nil {
				return cfg, err
			}
			switch v {
			case "both", "proxy", "statusline":
				cfg.Source = v
			default:
				return cfg, fmt.Errorf("invalid --source %q (must be both, proxy, or statusline)", v)
			}
		case "--data-dir":
			v, err := requireArg(qmArgs, &i, "--data-dir")
			if err != nil {
				return cfg, err
			}
			cfg.DataDir = v
		case "--quiet":
			cfg.Quiet = true
		case "--help", "-h":
			runHelp()
			os.Exit(0)
		default:
			return cfg, fmt.Errorf("unknown flag %q\nRun 'quotamaxxer --help' for usage.", qmArgs[i])
		}
		i++
	}

	return cfg, nil
}

func requireArg(args []string, i *int, flag string) (string, error) {
	*i++
	if *i >= len(args) {
		return "", fmt.Errorf("%s requires a value", flag)
	}
	return args[*i], nil
}
