package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// needsProxy reports whether a claude launch should run behind the
// in-process proxy.
//
// Sessions that render a UI (interactive, and background agents started with
// --bg) record usage through the statusline instead, and must not get an
// ANTHROPIC_BASE_URL pointing at this process: Claude Code hands that URL on
// to background agents and to its on-demand background daemon, which outlive
// quotamaxxer and would then talk to a dead port.
//
// Headless runs (-p/--print, or no TTY to draw a UI on) have no statusline,
// so the proxy is their only source of usage data. It lives exactly as long
// as the run.
//
// Only claude arguments before a literal "--" are considered flags.
func needsProxy(claudeArgs []string, stdinTTY, stdoutTTY bool) bool {
	printMode := false
	for _, a := range claudeArgs {
		if a == "--" {
			break
		}
		switch a {
		case "--bg", "--background":
			return false
		case "-p", "--print":
			printMode = true
		}
	}
	return printMode || !stdinTTY || !stdoutTTY
}

// statuslineConfigured reports whether a Claude Code settings file that
// applies to a session in cwd has a statusline that runs
// `quotamaxxer statusline` (directly or with --persist-only). It checks the
// user settings ($CLAUDE_CONFIG_DIR or ~/.claude) and the project's
// .claude/settings.json and .claude/settings.local.json.
//
// This is a heuristic used only to decide whether to print a setup hint:
// missing or unparseable files count as "not configured".
func statuslineConfigured(cwd string) bool {
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.Getenv("HOME")
		}
		configDir = filepath.Join(home, ".claude")
	}
	for _, path := range []string{
		filepath.Join(configDir, "settings.json"),
		filepath.Join(cwd, ".claude", "settings.json"),
		filepath.Join(cwd, ".claude", "settings.local.json"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var s struct {
			StatusLine struct {
				Command string `json:"command"`
			} `json:"statusLine"`
		}
		if json.Unmarshal(data, &s) != nil {
			continue
		}
		if strings.Contains(s.StatusLine.Command, "quotamaxxer") &&
			strings.Contains(s.StatusLine.Command, "statusline") {
			return true
		}
	}
	return false
}
