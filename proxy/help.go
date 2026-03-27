package main

import "fmt"

const helpText = `quotamaxxer — rate limit visibility for Claude Code

Usage:
  quotamaxxer [flags] [-- claude-args...]    Start proxy, optionally guard, then run claude
  quotamaxxer guard [flags]                  Wait for rate limits, then exit

Wrapper flags (before --):
  --threshold-5h <ratio>   Wait until 5h burn ratio drops below this (e.g. 0.8)
  --threshold-7d <ratio>   Wait until 7d burn ratio drops below this (e.g. 0.9)
  --wait-timeout <dur>     Max guard wait time (e.g. 30m, 1h). 0 = forever (default)
  --run-timeout <dur>      Max claude run time (e.g. 2h). 0 = forever (default)
  --source <src>           Data source: both (default), proxy, statusline
  --quiet                  Suppress waiting output
  --help                   Show this help

Guard flags:
  --threshold-5h, --threshold-7d, --wait-timeout, --source, --quiet
  Exits 0 when clear, 1 on timeout.

Burn ratio = used_pct / elapsed_pct, where elapsed_pct is the fraction of the
rate limit window already passed. A ratio of 1.0 means on pace to exhaust quota
at reset; >1.0 means burning too fast.

The guard reads usage-proxy.json (written by the proxy) and/or
usage-statusline.json (written by the statusline). Use --source to restrict
which file(s) are consulted. The most recent by timestamp is used.

Examples:
  quotamaxxer -p "hello"                                    # proxy + claude, no guard
  quotamaxxer --threshold-5h 0.8 -- -p "hello"              # guard, then proxy + claude
  quotamaxxer --run-timeout 2h -- -p "hello"                 # kill claude after 2h
  quotamaxxer guard --threshold-5h 0.8 --wait-timeout 30m   # standalone guard
  quotamaxxer -- -- -p "hello"                               # pass literal -- to claude`

func runHelp() {
	fmt.Println(helpText)
}
