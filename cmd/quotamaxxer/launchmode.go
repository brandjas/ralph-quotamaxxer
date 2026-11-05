package main

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
