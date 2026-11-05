package main

import "fmt"

const helpText = `quotamaxxer — rate limit visibility for Claude Code

Usage:
  quotamaxxer [flags] [-- claude-args...]    Optionally guard, then run claude
  quotamaxxer guard [flags]                  Wait for rate limits, then exit
  quotamaxxer monitor [flags]                Live TUI dashboard for rate limits
  quotamaxxer proxy                          Run standalone proxy server
  quotamaxxer statusline [--persist-only]    Claude Code statusline (reads JSON from stdin);
                                             --persist-only records usage without printing

Flags (must come first; the first unrecognized argument and everything after
it go to claude, or use -- to separate explicitly):
  --threshold-5h <ratio>   Wait until 5h burn ratio drops below this (e.g. 0.8)
  --threshold-7d <ratio>   Wait until 7d burn ratio drops below this (e.g. 0.9)
  --wait-timeout <dur>     Max guard wait time (e.g. 30m, 1h). 0 = forever (default)
  --run-timeout <dur>      Max claude run time (e.g. 2h); SIGTERM, then SIGKILL after
                           10s. Exits 124. Headless only. 0 = forever (default)
  --source <src>           Data source: both (default), proxy, statusline
  --data-dir <path>        Data directory (default: $QUOTAMAXXER_DATA_DIR, else
                           ~/.claude/ralph-quotamaxxer/data)
  --claude-command <cmd>   Command to run instead of claude (default: claude)
  --quiet                  Suppress guard output and the statusline setup hint
  --help                   Show this help

Guard flags:
  --threshold-5h, --threshold-7d, --wait-timeout, --source, --data-dir, --quiet
  Exits 0 when clear, 1 on timeout.

Burn ratio = used_pct / elapsed_pct, where elapsed_pct is the fraction of the
rate limit window already passed. A ratio of 1.0 means on pace to exhaust quota
at reset; >1.0 means burning too fast.

Usage tracking: headless runs (-p, or no terminal) go through an in-process
proxy that records rate limit headers. Interactive sessions and background
agents are recorded by the statusline instead; configure it to run
'quotamaxxer statusline' (or add 'quotamaxxer statusline --persist-only' to
your own statusline). See the README's Statusline section.

The guard reads usage-proxy.json (written by the proxy) and/or
usage-statusline.json (written by the statusline). Use --source to restrict
which file(s) are consulted. The most recent by timestamp is used.

Examples:
  quotamaxxer -p "hello"                                    # proxy + claude, no guard
  quotamaxxer --threshold-5h 0.8 -p "hello"                 # guard, then proxy + claude
  quotamaxxer --run-timeout 2h -- -p "hello"                 # kill claude after 2h
  quotamaxxer guard --threshold-5h 0.8 --wait-timeout 30m   # standalone guard
  quotamaxxer -- -- -p "hello"                               # pass literal -- to claude`

func runHelp() {
	fmt.Println(helpText)
}
