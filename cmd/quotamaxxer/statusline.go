package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"
)

// claudeCodeStatus is the JSON schema Claude Code pipes to the statusline command.
type claudeCodeStatus struct {
	Model struct {
		DisplayName string `json:"display_name"`
		ID          string `json:"id"`
	} `json:"model"`
	SessionID     string `json:"session_id"`
	ContextWindow struct {
		UsedPercentage float64 `json:"used_percentage"`
	} `json:"context_window"`
	Cost struct {
		TotalCostUSD float64 `json:"total_cost_usd"`
	} `json:"cost"`
	RateLimits *struct {
		FiveHour *claudeCodeWindow `json:"five_hour"`
		SevenDay *claudeCodeWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

// claudeCodeWindow is one rate limit window in Claude Code's statusline JSON.
// Either window may be absent.
type claudeCodeWindow struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"`
}

// statuslineWindow is a rate limit window prepared for display and persistence.
type statuslineWindow struct {
	Pct      float64 // rounded used percentage
	ResetsAt int64
	Burn     float64
}

// newStatuslineWindow returns nil if the window is absent or has no reset time.
func newStatuslineWindow(w *claudeCodeWindow, windowDuration int64) *statuslineWindow {
	if w == nil || w.ResetsAt <= 0 {
		return nil
	}
	pct := math.Round(w.UsedPercentage)
	return &statuslineWindow{
		Pct:      pct,
		ResetsAt: w.ResetsAt,
		Burn:     statuslineBurnRatio(pct, w.ResetsAt, windowDuration),
	}
}

// statuslinePersistData is the JSON schema written to usage-statusline.json.
type statuslinePersistData struct {
	Source    string `json:"source"`
	Timestamp struct {
		ISO   string `json:"iso"`
		Epoch int64  `json:"epoch"`
	} `json:"timestamp"`
	Hostname  string `json:"hostname"`
	SessionID string `json:"session_id"`
	Model     struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"model"`
	ContextPct float64 `json:"context_pct"`
	CostUSD    float64 `json:"cost_usd"`
	RateLimits struct {
		FiveHour struct {
			UsedPct   float64 `json:"used_pct"`
			ResetsAt  int64   `json:"resets_at"`
			BurnRatio float64 `json:"burn_ratio"`
		} `json:"five_hour"`
		SevenDay struct {
			UsedPct   float64 `json:"used_pct"`
			ResetsAt  int64   `json:"resets_at"`
			BurnRatio float64 `json:"burn_ratio"`
		} `json:"seven_day"`
	} `json:"rate_limits"`
}

// ANSI escape codes.
const (
	ansiRed    = "\033[0;31m"
	ansiYellow = "\033[0;33m"
	ansiGreen  = "\033[0;32m"
	ansiDim    = "\033[2m"
	ansiReset  = "\033[0m"
)

func colorForPct(pct float64) string {
	if pct > 80 {
		return ansiRed
	}
	if pct > 50 {
		return ansiYellow
	}
	return ansiGreen
}

func burnIndicator(ratio float64) string {
	if ratio > 0.95 {
		return "▲"
	}
	if ratio < 0.8 {
		return "▼"
	}
	return "●"
}

func statuslineBurnRatio(usedPct float64, resetsAt int64, windowDuration int64) float64 {
	ratio := computeBurnRatio(usedPct/100, resetsAt, windowDuration)
	return math.Round(ratio*100) / 100
}

// runStatusline reads raw Claude Code JSON from stdin, persists usage data,
// and prints the formatted statusline to stdout. With --persist-only it
// prints nothing, so a custom statusline command can call it purely to
// record usage.
func runStatusline(args []string) {
	fs := flag.NewFlagSet("statusline", flag.ContinueOnError)
	persistOnly := fs.Bool("persist-only", false, "record usage data without printing a statusline")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	dataDir := resolveDataDir()

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer statusline: read stdin: %v\n", err)
		os.Exit(1)
	}

	var status claudeCodeStatus
	if err := json.Unmarshal(input, &status); err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer statusline: parse JSON: %v\n", err)
		os.Exit(1)
	}

	// Build display line.
	cost := fmt.Sprintf("%.2f", status.Cost.TotalCostUSD)
	contextPct := int(math.Round(status.ContextWindow.UsedPercentage))
	line := fmt.Sprintf("%s%s%s | Ctx %d%% | $%s",
		ansiDim, status.Model.DisplayName, ansiReset, contextPct, cost)

	var fh, sd *statuslineWindow
	if status.RateLimits != nil {
		fh = newStatuslineWindow(status.RateLimits.FiveHour, window5hSeconds)
		sd = newStatuslineWindow(status.RateLimits.SevenDay, window7dSeconds)
	}
	for _, w := range []struct {
		label string
		win   *statuslineWindow
	}{{"5h", fh}, {"7d", sd}} {
		if w.win != nil {
			line += fmt.Sprintf(" | %s%s: %.0f%%%s%s",
				colorForPct(w.win.Pct), w.label, w.win.Pct, burnIndicator(w.win.Burn), ansiReset)
		}
	}
	if fh != nil || sd != nil {
		persistStatuslineData(dataDir, &status, time.Now(), fh, sd)
	}

	if !*persistOnly {
		fmt.Println(line)
	}
}

// persistStatuslineData writes the snapshot and history record. Absent
// windows (nil) are written as zeros, which the guard skips (reset <= 0).
func persistStatuslineData(dataDir string, status *claudeCodeStatus, now time.Time, fh, sd *statuslineWindow) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return
	}

	hostname, _ := os.Hostname()

	var data statuslinePersistData
	data.Source = "statusline"
	data.Timestamp.ISO = now.UTC().Format(time.RFC3339)
	data.Timestamp.Epoch = now.Unix()
	data.Hostname = hostname
	data.SessionID = status.SessionID
	data.Model.ID = status.Model.ID
	data.Model.Name = status.Model.DisplayName
	data.ContextPct = math.Round(status.ContextWindow.UsedPercentage)
	data.CostUSD = status.Cost.TotalCostUSD

	if fh != nil {
		data.RateLimits.FiveHour.UsedPct = fh.Pct
		data.RateLimits.FiveHour.ResetsAt = fh.ResetsAt
		data.RateLimits.FiveHour.BurnRatio = fh.Burn
	}
	if sd != nil {
		data.RateLimits.SevenDay.UsedPct = sd.Pct
		data.RateLimits.SevenDay.ResetsAt = sd.ResetsAt
		data.RateLimits.SevenDay.BurnRatio = sd.Burn
	}

	snapshotPath := filepath.Join(dataDir, "usage-statusline.json")
	historyPath := filepath.Join(dataDir, "usage-history.jsonl")
	persistAndAppendHistory(snapshotPath, historyPath, data, maxHistoryBytesFromEnv())
}
