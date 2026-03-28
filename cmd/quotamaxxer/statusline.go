package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
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
		FiveHour *struct {
			UsedPercentage float64 `json:"used_percentage"`
			ResetsAt       int64   `json:"resets_at"`
		} `json:"five_hour"`
		SevenDay *struct {
			UsedPercentage float64 `json:"used_percentage"`
			ResetsAt       int64   `json:"resets_at"`
		} `json:"seven_day"`
	} `json:"rate_limits"`
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

func statuslineBurnRatio(usedPct float64, resetsAt int64, now int64, windowDuration int64) float64 {
	elapsedPct := float64(windowDuration-(resetsAt-now)) / float64(windowDuration) * 100
	if elapsedPct < 0.1 {
		elapsedPct = 0.1
	}
	ratio := usedPct / elapsedPct
	return math.Round(ratio*100) / 100
}

// runStatusline reads raw Claude Code JSON from stdin, persists usage data,
// and prints the formatted statusline to stdout.
func runStatusline() {
	dataDir := envOr("QUOTAMAXXER_DATA_DIR", resolveDefaultDataDir())

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

	hasRateLimits := status.RateLimits != nil &&
		status.RateLimits.FiveHour != nil &&
		status.RateLimits.FiveHour.ResetsAt > 0

	if hasRateLimits {
		now := time.Now()

		fhPct := math.Round(status.RateLimits.FiveHour.UsedPercentage)
		sdPct := math.Round(status.RateLimits.SevenDay.UsedPercentage)
		burn5h := statuslineBurnRatio(fhPct, status.RateLimits.FiveHour.ResetsAt, now.Unix(), window5hSeconds)
		burn7d := statuslineBurnRatio(sdPct, status.RateLimits.SevenDay.ResetsAt, now.Unix(), window7dSeconds)

		line += fmt.Sprintf(" | %s5h: %.0f%%%s%s", colorForPct(fhPct), fhPct, burnIndicator(burn5h), ansiReset)
		line += fmt.Sprintf(" | %s7d: %.0f%%%s%s", colorForPct(sdPct), sdPct, burnIndicator(burn7d), ansiReset)

		// Persist usage data.
		persistStatuslineData(dataDir, &status, now, fhPct, sdPct, burn5h, burn7d)
	}

	fmt.Println(line)
}

func persistStatuslineData(dataDir string, status *claudeCodeStatus, now time.Time, fhPct, sdPct, burn5h, burn7d float64) {
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

	if fh := status.RateLimits.FiveHour; fh != nil {
		data.RateLimits.FiveHour.UsedPct = fhPct
		data.RateLimits.FiveHour.ResetsAt = fh.ResetsAt
		data.RateLimits.FiveHour.BurnRatio = burn5h
	}
	if sd := status.RateLimits.SevenDay; sd != nil {
		data.RateLimits.SevenDay.UsedPct = sdPct
		data.RateLimits.SevenDay.ResetsAt = sd.ResetsAt
		data.RateLimits.SevenDay.BurnRatio = burn7d
	}

	pretty, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}
	pretty = append(pretty, '\n')

	// Write usage-statusline.json atomically.
	snapshotPath := filepath.Join(dataDir, "usage-statusline.json")
	tmp := snapshotPath + ".tmp"
	if err := os.WriteFile(tmp, pretty, 0644); err != nil {
		return
	}
	os.Rename(tmp, snapshotPath)

	// Append compact JSON to history.
	compact, _ := json.Marshal(data)
	compact = append(compact, '\n')

	historyPath := filepath.Join(dataDir, "usage-history.jsonl")
	lockPath := historyPath + ".lock"

	maxHistoryBytes := int64(10 * 1024 * 1024)
	if v := os.Getenv("QUOTAMAXXER_MAX_HISTORY_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			maxHistoryBytes = n
		}
	}

	appendHistory(historyPath, lockPath, compact, maxHistoryBytes)
}
