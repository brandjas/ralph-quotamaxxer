# Ralph Quotamaxxer

Rate limit visibility for Claude Code Pro/Max subscribers.

Claude Code doesn't expose rate limit data in headless (`claude -p`) sessions or to external tooling. Ralph Quotamaxxer fills that gap with a transparent proxy that extracts rate limit headers from every API response, and a statusline that displays them in interactive sessions.

## Quick start

```bash
curl -fsSL https://raw.githubusercontent.com/brandjas/ralph-quotamaxxer/main/install-remote.sh | bash
```

No Go required — downloads a pre-built binary for your platform. Pin a version with `QUOTAMAXXER_VERSION=v0.1.0`.

<details>
<summary>Install from source (requires Go 1.22+)</summary>

```bash
git clone https://github.com/brandjas/ralph-quotamaxxer.git
cd ralph-quotamaxxer
./install.sh
```
</details>

Then use `quotamaxxer` instead of `claude`:

```bash
~/.claude/ralph-quotamaxxer/bin/quotamaxxer          # interactive
~/.claude/ralph-quotamaxxer/bin/quotamaxxer -p "..."  # headless
```

Or alias it:

```bash
alias claude=~/.claude/ralph-quotamaxxer/bin/quotamaxxer
```

`quotamaxxer` is a drop-in replacement for `claude` — all arguments are forwarded as-is.

## Rate limit guard

The guard pauses execution until your burn rate drops below a threshold. Burn ratio = usage / elapsed fraction of the window. A ratio of 1.0 means on pace to exhaust quota at reset; >1.0 means burning too fast.

**Integrated** — guard before each run:

```bash
quotamaxxer --threshold-5h 0.8 --threshold-7d 0.9 -- -p "..."
```

**Standalone** — useful in loops:

```bash
while true; do
  quotamaxxer guard --threshold-5h 0.8 --threshold-7d 0.9
  claude -p "..."
done
```

**Flags** (before `--`, or after `guard`):

| Flag | Description |
|---|---|
| `--threshold-5h <ratio>` | Wait until 5h burn ratio drops below this |
| `--threshold-7d <ratio>` | Wait until 7d burn ratio drops below this |
| `--timeout <duration>` | Max wait time (e.g. `30m`, `1h`). Exit 1 if exceeded. Default: forever |
| `--source <src>` | Data source: `both` (default), `proxy`, `statusline` |
| `--quiet` | Suppress waiting output |
| `--help` | Show help |

Without `--`, all arguments go directly to `claude` (no guard runs). The `--` separator splits quotamaxxer flags from claude arguments.

## What you get

After each API call, rate limit data is written to `~/.claude/ralph-quotamaxxer/data/`:

| File | Purpose |
|---|---|
| `usage-proxy.json` | Latest rate limit snapshot from the proxy (pretty-printed, atomic write) |
| `usage-statusline.json` | Latest session state from the statusline |
| `usage-history.jsonl` | Append-only history from both sources (auto-rotating at 10 MB) |

In interactive sessions, the statusline also displays model, context usage, cost, and color-coded rate limit percentages with burn rate indicators.

All of `~/.claude/` is bind-mounted into devcontainers, so every container shares the same data.

## Configuration

All via environment variables — no config files:

| Variable | Default | Purpose |
|---|---|---|
| `QUOTAMAXXER_DATA_DIR` | `~/.claude/ralph-quotamaxxer/data` | Data directory |
| `QUOTAMAXXER_UPSTREAM` | `https://api.anthropic.com` | Upstream URL (useful for testing) |
| `QUOTAMAXXER_PORT` | `0` (OS-assigned) | Proxy listen port |
| `QUOTAMAXXER_MAX_HISTORY_BYTES` | `10485760` (10 MB) | History rotation threshold |

## Testing

```bash
./test/test-proxy.sh
```

Builds the proxy, fires a real Haiku call through it, and validates that `usage-proxy.json` and `usage-history.jsonl` were written correctly.

```bash
./test/test-guard.sh
```

Tests the guard subcommand with synthetic data: low/high utilization, timeout behavior, both file formats, `--source` filtering.

## History schema

Both the proxy and statusline append to `usage-history.jsonl`. Every record shares a common core:

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

The `source` field distinguishes record origin. Each source adds fields the other can't provide:

- **Proxy-only:** `representative_claim`, `overage`, `raw_headers` — from HTTP response headers.
- **Statusline-only:** `hostname`, `session_id`, `model`, `context_pct`, `cost`, `burn_ratio` — from Claude Code's stdin JSON.

Consumers can filter on `source` for source-specific fields, or ignore it to process rate limit data uniformly.

## How it works

**Proxy** — A Go reverse proxy (`net/http/httputil.ReverseProxy`) that sits between Claude Code and `api.anthropic.com`. Extracts `anthropic-ratelimit-unified-*` response headers on every response. Writes the latest snapshot to `usage-proxy.json` and appends to the shared history. Go standard library only, zero external dependencies.

**Statusline** — Bash scripts invoked by Claude Code's statusline mechanism after each assistant message. Reads rate limit data from Claude Code's stdin JSON in interactive sessions, falling back to `usage-proxy.json` for headless sessions.

**Guard** — Built into the Go binary as a `guard` subcommand. Reads `usage-proxy.json` and/or `usage-statusline.json`, computes burn ratios, and sleeps until both windows are below threshold. Re-reads data every 30s to account for usage changes from other sessions.

**Wrapper** — The `quotamaxxer` script manages the proxy lifecycle: binds to `:0` for an OS-assigned ephemeral port, starts the proxy in the background, reads the assigned port back via a temp file, then `exec`s `claude` with `ANTHROPIC_BASE_URL` set. When `--threshold-*` flags are present, runs the guard before starting the proxy. The proxy lives and dies with the claude process — no daemons, no PID files, no port conflicts.

The proxy is transparent: no body inspection, no header mutation, SSE streaming works natively (Go 1.20+ auto-flushes `text/event-stream`), and upstream errors (including 429s with rate limit headers) are passed through as-is.

## References

1. [Claude Code — Statusline docs](https://code.claude.com/docs/en/statusline) — "Your script runs after each new assistant message" (interactive TUI only).
2. [Claude Code — Environment variables](https://code.claude.com/docs/en/env-vars) — `ANTHROPIC_BASE_URL` sets the SDK's `baseURL`; Claude Code appends API paths to it.
3. [Go `httputil.ReverseProxy`](https://pkg.go.dev/net/http/httputil#ReverseProxy) — `Director` rewrites requests, `ModifyResponse` intercepts responses before streaming.
4. [Go #47359 — ReverseProxy SSE auto-flush](https://github.com/golang/go/issues/47359) — Since Go 1.20, `ReverseProxy` detects `text/event-stream` and flushes immediately. This is why Go 1.20+ is required.
5. Claude Code source (v2.1.84) — The `OX8` function reads `anthropic-ratelimit-unified-*` headers on every streaming response. On 429 errors, `EN$` parses the same headers to identify the exhausted window. Window durations are hardcoded: `five_hour` = 18,000s, `seven_day` = 604,800s.
6. [Anthropic — Rate limits](https://docs.anthropic.com/en/api/rate-limits) — Documents standard API rate limit headers. The `unified-*` headers are undocumented and specific to consumer-tier (Pro/Max) subscriptions.
7. [claude-code#33820](https://github.com/anthropics/claude-code/issues/33820) — Feature request to expose rate limit headers to hooks (motivation for this project).

## License

MIT
