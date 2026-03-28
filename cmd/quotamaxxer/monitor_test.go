package main

import (
	"math"
	"testing"
)

func approxEqual(a, b []float64, tolerance float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > tolerance {
			return false
		}
	}
	return true
}

func TestBucketHistory(t *testing.T) {
	pct := func(r historyRecord) float64 { return r.fiveHourPct() }

	tests := []struct {
		name       string
		records    []historyRecord
		windowSec  int64
		numBuckets int
		now        int64
		want       []float64
	}{
		{
			name:       "empty records returns all zeros",
			records:    nil,
			windowSec:  100,
			numBuckets: 5,
			now:        1000,
			want:       []float64{0, 0, 0, 0, 0},
		},
		{
			name: "single record sampled at its time and held right",
			records: []historyRecord{
				{Source: "proxy", Epoch: 960, FiveHour: &windowData{Utilization: 0.5}},
			},
			windowSec:  100,
			numBuckets: 5,
			now:        1000,
			// Column sample times: 920, 940, 960, 980, 1000
			// Record at 960: columns 0,1 have no record before them → 0
			// Columns 2,3,4 see record at 960 → 50
			want: []float64{0, 0, 50, 50, 50},
		},
		{
			name: "later record overrides earlier at same sample point",
			records: []historyRecord{
				{Source: "proxy", Epoch: 910, FiveHour: &windowData{Utilization: 0.3}},
				{Source: "proxy", Epoch: 915, FiveHour: &windowData{Utilization: 0.1}},
			},
			windowSec:  100,
			numBuckets: 5,
			now:        1000,
			// Column sample times: 920, 940, 960, 980, 1000
			// Both records at 910,915 are before column 0 (920)
			// Most recent at each sample point is 915 → 10
			want: []float64{10, 10, 10, 10, 10},
		},
		{
			name: "sample-and-hold across gap",
			records: []historyRecord{
				{Source: "proxy", Epoch: 905, FiveHour: &windowData{Utilization: 0.0}},
				{Source: "proxy", Epoch: 985, FiveHour: &windowData{Utilization: 1.0}},
			},
			windowSec:  100,
			numBuckets: 5,
			now:        1000,
			// Column sample times: 920, 940, 960, 980, 1000
			// Columns 0-3: most recent is 905 → 0
			// Column 4 (1000): most recent is 985 → 100
			want: []float64{0, 0, 0, 0, 100},
		},
		{
			name: "out-of-window records excluded",
			records: []historyRecord{
				{Source: "proxy", Epoch: 800, FiveHour: &windowData{Utilization: 0.9}},
				{Source: "proxy", Epoch: 950, FiveHour: &windowData{Utilization: 0.2}},
			},
			windowSec:  100,
			numBuckets: 5,
			now:        1000,
			// 800 excluded. Column times: 920, 940, 960, 980, 1000
			// Columns 0,1: no record → 0
			// Columns 2,3,4: most recent is 950 → 20
			want: []float64{0, 0, 20, 20, 20},
		},
		{
			name: "statusline records use used_pct directly",
			records: []historyRecord{
				{Source: "statusline", Timestamp: struct {
					Epoch int64 `json:"epoch"`
				}{Epoch: 950}, RateLimits: struct {
					FiveHour struct {
						UsedPct float64 `json:"used_pct"`
					} `json:"five_hour"`
					SevenDay struct {
						UsedPct float64 `json:"used_pct"`
					} `json:"seven_day"`
				}{FiveHour: struct {
					UsedPct float64 `json:"used_pct"`
				}{UsedPct: 42}}},
			},
			windowSec:  100,
			numBuckets: 5,
			now:        1000,
			// Column times: 920, 940, 960, 980, 1000
			// Columns 0,1: no record → 0
			// Columns 2,3,4: record at 950 → 42
			want: []float64{0, 0, 42, 42, 42},
		},
		{
			name: "no backward extrapolation before first point",
			records: []historyRecord{
				{Source: "proxy", Epoch: 965, FiveHour: &windowData{Utilization: 0.5}},
				{Source: "proxy", Epoch: 990, FiveHour: &windowData{Utilization: 0.8}},
			},
			windowSec:  100,
			numBuckets: 5,
			now:        1000,
			// Column times: 920, 940, 960, 980, 1000
			// Columns 0,1,2: no record before → 0
			// Column 3 (980): most recent is 965 → 50
			// Column 4 (1000): most recent is 990 → 80
			want: []float64{0, 0, 0, 50, 80},
		},
		{
			name: "three points sampled correctly",
			records: []historyRecord{
				{Source: "proxy", Epoch: 905, FiveHour: &windowData{Utilization: 0.0}},
				{Source: "proxy", Epoch: 945, FiveHour: &windowData{Utilization: 0.6}},
				{Source: "proxy", Epoch: 985, FiveHour: &windowData{Utilization: 0.2}},
			},
			windowSec:  100,
			numBuckets: 5,
			now:        1000,
			// Column times: 920, 940, 960, 980, 1000
			// Col 0 (920): most recent is 905 → 0
			// Col 1 (940): most recent is 905 → 0
			// Col 2 (960): most recent is 945 → 60
			// Col 3 (980): most recent is 945 → 60
			// Col 4 (1000): most recent is 985 → 20
			want: []float64{0, 0, 60, 60, 20},
		},
		{
			name: "reset transition is stable regardless of bucket alignment",
			records: []historyRecord{
				{Source: "proxy", Epoch: 941, FiveHour: &windowData{Utilization: 0.4}},
				{Source: "proxy", Epoch: 959, FiveHour: &windowData{Utilization: 0.02}},
			},
			windowSec:  100,
			numBuckets: 5,
			now:        1000,
			// Column times: 920, 940, 960, 980, 1000
			// Col 0,1: no record → 0
			// Col 2 (960): most recent is 959 → 2
			// Col 3,4: most recent is 959 → 2
			want: []float64{0, 0, 2, 2, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bucketHistory(tt.records, tt.windowSec, tt.numBuckets, tt.now, pct)
			if !approxEqual(got, tt.want, 0.5) {
				t.Errorf("bucketHistory() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBucketShiftStability(t *testing.T) {
	// Verify that shifting 'now' by 1 second produces at most 1 column diff.
	pct := func(r historyRecord) float64 { return r.fiveHourPct() }
	records := []historyRecord{
		{Source: "proxy", Epoch: 910, FiveHour: &windowData{Utilization: 0.5}},
		{Source: "proxy", Epoch: 970, FiveHour: &windowData{Utilization: 0.1}},
	}

	a := bucketHistory(records, 100, 10, 1000, pct)
	b := bucketHistory(records, 100, 10, 1001, pct)

	diffs := 0
	for i := range a {
		if math.Abs(a[i]-b[i]) > 0.5 {
			diffs++
		}
	}
	// At most 1 column changes when a record epoch crosses a sample time.
	if diffs > 1 {
		t.Errorf("bucket shift caused %d diffs (want <=1):\n  now=1000: %v\n  now=1001: %v", diffs, a, b)
	}
}
