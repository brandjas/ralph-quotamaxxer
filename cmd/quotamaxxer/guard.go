package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	window5hSeconds = 18000  // 5 hours
	window7dSeconds = 604800 // 7 days
)

// usageSnapshot normalizes data from both proxy and statusline file formats.
type usageSnapshot struct {
	Epoch         int64
	FiveHourUtil  float64 // 0.0–1.0
	FiveHourReset int64   // unix epoch
	SevenDayUtil  float64
	SevenDayReset int64
}

// proxyFileData matches the schema of usage-proxy.json.
type proxyFileData struct {
	Epoch    int64       `json:"epoch"`
	FiveHour *windowData `json:"five_hour"`
	SevenDay *windowData `json:"seven_day"`
}

// statuslineFileData matches the schema of usage-statusline.json.
type statuslineFileData struct {
	Timestamp struct {
		Epoch int64 `json:"epoch"`
	} `json:"timestamp"`
	RateLimits struct {
		FiveHour struct {
			UsedPct  float64 `json:"used_pct"`
			ResetsAt int64   `json:"resets_at"`
		} `json:"five_hour"`
		SevenDay struct {
			UsedPct  float64 `json:"used_pct"`
			ResetsAt int64   `json:"resets_at"`
		} `json:"seven_day"`
	} `json:"rate_limits"`
}

func readProxyFile(path string) (usageSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return usageSnapshot{}, err
	}
	var pf proxyFileData
	if err := json.Unmarshal(data, &pf); err != nil {
		return usageSnapshot{}, fmt.Errorf("parse %s: %w", path, err)
	}
	snap := usageSnapshot{Epoch: pf.Epoch}
	if pf.FiveHour != nil {
		snap.FiveHourUtil = pf.FiveHour.Utilization
		snap.FiveHourReset = pf.FiveHour.Reset
	}
	if pf.SevenDay != nil {
		snap.SevenDayUtil = pf.SevenDay.Utilization
		snap.SevenDayReset = pf.SevenDay.Reset
	}
	return snap, nil
}

func readStatuslineFile(path string) (usageSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return usageSnapshot{}, err
	}
	var sf statuslineFileData
	if err := json.Unmarshal(data, &sf); err != nil {
		return usageSnapshot{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return usageSnapshot{
		Epoch:         sf.Timestamp.Epoch,
		FiveHourUtil:  sf.RateLimits.FiveHour.UsedPct / 100.0,
		FiveHourReset: sf.RateLimits.FiveHour.ResetsAt,
		SevenDayUtil:  sf.RateLimits.SevenDay.UsedPct / 100.0,
		SevenDayReset: sf.RateLimits.SevenDay.ResetsAt,
	}, nil
}

func readLatestUsage(dataDir, source string) (usageSnapshot, error) {
	proxyPath := filepath.Join(dataDir, "usage-proxy.json")
	statuslinePath := filepath.Join(dataDir, "usage-statusline.json")

	var snapProxy, snapStatusline usageSnapshot
	var errProxy, errStatusline error

	if source == "both" || source == "proxy" {
		snapProxy, errProxy = readProxyFile(proxyPath)
	} else {
		errProxy = fmt.Errorf("skipped")
	}

	if source == "both" || source == "statusline" {
		snapStatusline, errStatusline = readStatuslineFile(statuslinePath)
	} else {
		errStatusline = fmt.Errorf("skipped")
	}

	haveProxy := errProxy == nil
	haveStatusline := errStatusline == nil

	if !haveProxy && !haveStatusline {
		return usageSnapshot{}, fmt.Errorf("no usage data found")
	}
	if !haveProxy {
		return snapStatusline, nil
	}
	if !haveStatusline {
		return snapProxy, nil
	}
	// Both available — pick most recent.
	if snapStatusline.Epoch > snapProxy.Epoch {
		return snapStatusline, nil
	}
	return snapProxy, nil
}

func computeBurnRatio(util float64, resetEpoch int64, windowDuration int64) float64 {
	return util / elapsedPct(resetEpoch, windowDuration)
}

func computeSleepSeconds(util, threshold, elapsedPct float64, windowDuration int64) float64 {
	targetElapsedPct := util / threshold
	return (targetElapsedPct - elapsedPct) * float64(windowDuration)
}

func elapsedPct(resetEpoch int64, windowDuration int64) float64 {
	now := time.Now().Unix()
	secondsUntilReset := resetEpoch - now
	pct := float64(windowDuration-secondsUntilReset) / float64(windowDuration)
	if pct < 0.001 {
		pct = 0.001
	}
	return pct
}

func formatDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

type guardConfig struct {
	Threshold5h float64
	Threshold7d float64
	DataDir     string
	WaitTimeout time.Duration
	Quiet       bool
	Source      string
}

// checkWindowBurn checks whether a single rate-limit window exceeds its
// threshold and returns the burn ratio and required sleep duration.
func checkWindowBurn(util float64, resetEpoch, windowDuration int64, threshold float64, now int64) (burn, sleep float64) {
	burn = computeBurnRatio(util, resetEpoch, windowDuration)
	if burn <= threshold {
		return burn, 0
	}
	ep := elapsedPct(resetEpoch, windowDuration)
	sleep = computeSleepSeconds(util, threshold, ep, windowDuration)
	if untilReset := float64(resetEpoch - now); untilReset > 0 && sleep > untilReset {
		sleep = untilReset
	}
	return burn, sleep
}

// guardLoop runs the guard wait loop. Returns nil when clear, error on timeout.
func guardLoop(cfg guardConfig) error {
	deadline := time.Time{}
	if cfg.WaitTimeout > 0 {
		deadline = time.Now().Add(cfg.WaitTimeout)
	}

	for {
		snap, err := readLatestUsage(cfg.DataDir, cfg.Source)
		if err != nil {
			if !cfg.Quiet {
				fmt.Fprintln(os.Stderr, "quotamaxxer: no usage data found, proceeding")
			}
			return nil
		}

		now := time.Now().Unix()
		var maxSleep float64
		var worstWindow string
		var worstBurn, worstThreshold float64

		type windowCheck struct {
			name      string
			util      float64
			reset     int64
			window    int64
			threshold float64
		}
		for _, w := range []windowCheck{
			{"5h", snap.FiveHourUtil, snap.FiveHourReset, window5hSeconds, cfg.Threshold5h},
			{"7d", snap.SevenDayUtil, snap.SevenDayReset, window7dSeconds, cfg.Threshold7d},
		} {
			if w.threshold <= 0 || w.reset <= 0 {
				continue
			}
			burn, s := checkWindowBurn(w.util, w.reset, w.window, w.threshold, now)
			if s > maxSleep {
				maxSleep = s
				worstWindow = w.name
				worstBurn = burn
				worstThreshold = w.threshold
			}
		}

		if maxSleep <= 0 {
			if !cfg.Quiet {
				fmt.Fprintln(os.Stderr, "quotamaxxer: rate limits OK, proceeding")
			}
			return nil
		}

		if !deadline.IsZero() && time.Now().After(deadline) {
			return fmt.Errorf("quotamaxxer: timeout, %s burn ratio still %.2f (threshold %.2f)",
				worstWindow, worstBurn, worstThreshold)
		}

		sleepDur := maxSleep

		if !deadline.IsZero() {
			remaining := time.Until(deadline).Seconds()
			if remaining < sleepDur {
				sleepDur = remaining
			}
		}

		if sleepDur < 1 {
			sleepDur = 1
		}

		if !cfg.Quiet {
			fmt.Fprintf(os.Stderr, "quotamaxxer: waiting %s — %s burn %.2f > %.2f\n",
				formatDuration(sleepDur), worstWindow, worstBurn, worstThreshold)
		}

		time.Sleep(time.Duration(sleepDur * float64(time.Second)))
	}
}

func runGuard(args []string) {
	fs := flag.NewFlagSet("guard", flag.ExitOnError)
	threshold5h := fs.Float64("threshold-5h", 0, "5h burn ratio threshold")
	threshold7d := fs.Float64("threshold-7d", 0, "7d burn ratio threshold")
	dataDir := fs.String("data-dir", resolveDefaultDataDir(), "data directory")
	timeout := fs.Duration("wait-timeout", 0, "max wait time (0 = forever)")
	quiet := fs.Bool("quiet", false, "suppress output")
	source := fs.String("source", "proxy", "data source: proxy (default), both, statusline")
	fs.Parse(args)

	if *threshold5h == 0 && *threshold7d == 0 {
		fmt.Fprintln(os.Stderr, "quotamaxxer guard: at least one of --threshold-5h or --threshold-7d is required")
		os.Exit(1)
	}

	if err := validateSource(*source); err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer guard: %v\n", err)
		os.Exit(1)
	}

	cfg := guardConfig{
		Threshold5h: *threshold5h,
		Threshold7d: *threshold7d,
		DataDir:     *dataDir,
		WaitTimeout: *timeout,
		Quiet:       *quiet,
		Source:      *source,
	}

	if err := guardLoop(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
