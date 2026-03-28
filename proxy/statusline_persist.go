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

func statuslineBurnRatio(usedPct float64, resetsAt int64, now int64, windowDuration int64) float64 {
	elapsedPct := float64(windowDuration-(resetsAt-now)) / float64(windowDuration) * 100
	if elapsedPct < 0.1 {
		elapsedPct = 0.1
	}
	ratio := usedPct / elapsedPct
	return math.Round(ratio*100) / 100
}

// runStatuslinePersist reads raw Claude Code JSON from stdin, transforms it
// into the persistence format, and writes usage-statusline.json + history.
func runStatuslinePersist() {
	dataDir := envOr("QUOTAMAXXER_DATA_DIR", resolveDefaultDataDir())

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer statusline-persist: cannot create data dir: %v\n", err)
		os.Exit(1)
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer statusline-persist: read stdin: %v\n", err)
		os.Exit(1)
	}

	var status claudeCodeStatus
	if err := json.Unmarshal(input, &status); err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer statusline-persist: parse JSON: %v\n", err)
		os.Exit(1)
	}

	now := time.Now()
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

	if status.RateLimits != nil {
		if fh := status.RateLimits.FiveHour; fh != nil {
			data.RateLimits.FiveHour.UsedPct = math.Round(fh.UsedPercentage)
			data.RateLimits.FiveHour.ResetsAt = fh.ResetsAt
			data.RateLimits.FiveHour.BurnRatio = statuslineBurnRatio(
				math.Round(fh.UsedPercentage), fh.ResetsAt, now.Unix(), window5hSeconds)
		}
		if sd := status.RateLimits.SevenDay; sd != nil {
			data.RateLimits.SevenDay.UsedPct = math.Round(sd.UsedPercentage)
			data.RateLimits.SevenDay.ResetsAt = sd.ResetsAt
			data.RateLimits.SevenDay.BurnRatio = statuslineBurnRatio(
				math.Round(sd.UsedPercentage), sd.ResetsAt, now.Unix(), window7dSeconds)
		}
	}

	pretty, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer statusline-persist: marshal: %v\n", err)
		os.Exit(1)
	}
	pretty = append(pretty, '\n')

	// Write usage-statusline.json atomically.
	snapshotPath := filepath.Join(dataDir, "usage-statusline.json")
	tmp := snapshotPath + ".tmp"
	if err := os.WriteFile(tmp, pretty, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer statusline-persist: write: %v\n", err)
		os.Exit(1)
	}
	if err := os.Rename(tmp, snapshotPath); err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer statusline-persist: rename: %v\n", err)
		os.Exit(1)
	}

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
