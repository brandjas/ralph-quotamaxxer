<div align="center">

# Ralph Quotamaxxer

**You paid for the tokens. Use all of them.**

Rate limit-aware pacing for Claude Code. Lets autonomous loops burn right up
to your quota ceiling and coast until it resets.

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-linux%20%7C%20macOS-lightgrey)]()

[Install](#install) | [Usage](#usage) | [How it works](#how-it-works)

</div>

---

## What it is

A drop-in replacement for `claude` that tracks your rate limits. If your burn rate is too high, it waits for it to come back down. If you start using Claude yourself, ralph loops using `quotamaxxer` automatically back off to make room, then ramp back up when you stop.

A ralph loop looks like this:

```bash
while true; do
  # waits if you're burning quota too fast, runs immediately if there's headroom
  quotamaxxer --threshold-5h 0.8 --threshold-7d 0.9 -- -p "do work"
done
```

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/brandjas/ralph-quotamaxxer/main/install-remote.sh | bash
```

<details>
<summary><b>Install from source</b> (requires Go 1.22+)</summary>

```bash
git clone https://github.com/brandjas/ralph-quotamaxxer.git
cd ralph-quotamaxxer
./install.sh
```
</details>

<details>
<summary><b>Supported platforms</b></summary>

| OS | Arch |
|---|---|
| Linux | amd64, arm64 |
| macOS | amd64 (Intel), arm64 (Apple Silicon) |

</details>

Everything installs into `~/.claude/ralph-quotamaxxer/`, so it works inside devcontainers that bind-mount `~/.claude`. Multiple sessions (interactive, headless, across containers) can run concurrently and share the same usage data.

```bash
alias claude='~/.claude/ralph-quotamaxxer/bin/quotamaxxer --'
```

`quotamaxxer` is a single Go binary. It starts an in-process reverse proxy, runs `claude` as a child, and propagates exit codes correctly.

## Usage

> [!IMPORTANT]
> Usage data is only collected when Claude runs through `quotamaxxer`. If you run `claude` directly, those API calls won't be tracked and the guard will underestimate your actual usage. For accurate pacing, run **all** sessions through `quotamaxxer` — the simplest way is to [alias `claude`](#install).

Without threshold flags, it just proxies and records rate limits:

```bash
quotamaxxer              # interactive
quotamaxxer -p "..."     # headless
```

With threshold flags, it waits until your burn rate dips below the threshold before running:

```bash
quotamaxxer --threshold-5h 0.8 --threshold-7d 0.9 -- -p "..."
```

The guard is also available as a standalone command. It blocks until there's headroom, then exits:

```bash
quotamaxxer guard --threshold-5h 0.8 --threshold-7d 0.9 && \
echo "🚀 quota headroom available, let's go"
```


<details>
<summary><b>Flags</b> (before <code>--</code>, or after <code>guard</code>)</summary>

| Flag | Description |
|---|---|
| `--threshold-5h <ratio>` | Wait until 5h burn ratio drops below this |
| `--threshold-7d <ratio>` | Wait until 7d burn ratio drops below this |
| `--wait-timeout <dur>` | Max guard wait time (e.g. `30m`, `1h`). Exit 1 if exceeded. Default: forever |
| `--run-timeout <dur>` | Max claude run time (e.g. `2h`). Exit 124 if exceeded. Headless only. Default: forever |
| `--source <src>` | Data source: `both` (default), `proxy`, `statusline` |
| `--quiet` | Suppress waiting output |
| `--help` | Show help |

The `guard` subcommand requires at least one `--threshold-*` flag. All arguments after `--` go directly to `claude`.

</details>

## What you get

After each API call, rate limit data is written to `~/.claude/ralph-quotamaxxer/data/`:

| File | Contents |
|---|---|
| `usage-proxy.json` | Latest rate limit snapshot from the proxy |
| `usage-history.jsonl` | Append-only history (auto-rotates at 10 MB) |

The install also includes a statusline script that collects usage data from interactive sessions and displays quota percentages, model, context usage, and cost. To enable it, add this to `~/.claude/settings.json` (requires `jq`):

```json
{
  "statusLine": { "type": "command", "command": "~/.claude/ralph-quotamaxxer/statusline/statusline.sh" }
}
```

## Configuration

All via environment variables, no config files:

| Variable | Default | Purpose |
|---|---|---|
| `QUOTAMAXXER_DATA_DIR` | `~/.claude/ralph-quotamaxxer/data` | Data directory |
| `QUOTAMAXXER_UPSTREAM` | `https://api.anthropic.com` | Upstream URL |
| `QUOTAMAXXER_PORT` | `0` (OS-assigned) | Proxy listen port (standalone `proxy` subcommand only) |
| `QUOTAMAXXER_MAX_HISTORY_BYTES` | `10485760` (10 MB) | History rotation threshold |

## How it works

`quotamaxxer` is a single Go binary that starts an in-process reverse proxy on an ephemeral port, points Claude Code at it via `ANTHROPIC_BASE_URL`, then runs `claude` as a child process. The proxy sits between Claude Code and `api.anthropic.com`, extracting `anthropic-ratelimit-unified-*` headers from every response and writing them to disk. No body inspection, no header mutation, SSE streaming passes through, and upstream errors (including 429s) are forwarded as-is. Go standard library only.

The proxy is an in-process goroutine — no daemons, no PID files, no port conflicts, no orphaned processes.

<details>
<summary><b>History schema</b></summary>

Both sources append to `usage-history.jsonl` with a common core:

```json
{
  "source": "proxy | statusline",
  "timestamp": "2026-03-26T12:00:00Z",
  "epoch": 1774526400,
  "status": "allowed | allowed_warning | rejected",
  "five_hour": { "utilization": 0.04, "reset": 1774540800 },
  "seven_day": { "utilization": 0.77, "reset": 1774828800 }
}
```

Source-specific fields:

- **Proxy:** `representative_claim`, `overage`, `raw_headers`
- **Statusline:** `hostname`, `session_id`, `model`, `context_pct`, `cost`, `burn_ratio`

</details>

## Testing

```bash
./test/test-proxy.sh    # real Haiku call through the proxy
./test/test-guard.sh    # synthetic guard scenarios
```

<details>
<summary><b>References</b></summary>

1. [Claude Code - Statusline docs](https://code.claude.com/docs/en/statusline)
2. [Claude Code - Environment variables](https://code.claude.com/docs/en/env-vars): `ANTHROPIC_BASE_URL` sets the SDK's `baseURL`
3. [Go `httputil.ReverseProxy`](https://pkg.go.dev/net/http/httputil#ReverseProxy)
4. Claude Code source (v2.1.84): `OX8` reads `anthropic-ratelimit-unified-*` headers; `EN$` parses them on 429s. Window durations hardcoded: `five_hour` = 18,000s, `seven_day` = 604,800s
5. [Anthropic - Rate limits](https://docs.anthropic.com/en/api/rate-limits): `unified-*` headers are undocumented, specific to Pro/Max
6. [claude-code#33820](https://github.com/anthropics/claude-code/issues/33820): feature request motivating this project

</details>

<details>
<summary><b>Uninstall</b></summary>

```bash
rm -rf ~/.claude/ralph-quotamaxxer
```

</details>

## License

MIT
