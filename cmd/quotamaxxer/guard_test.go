package main

import "testing"

func TestMergeSnapshots(t *testing.T) {
	const r5, r7 = 1_000_000, 2_000_000
	tests := []struct {
		name string
		a, b usageSnapshot
		want usageSnapshot
	}{
		{
			name: "same window: higher utilization wins regardless of epoch",
			a:    usageSnapshot{Epoch: 100, FiveHourUtil: 0.9, FiveHourReset: r5, SevenDayUtil: 0.2, SevenDayReset: r7},
			b:    usageSnapshot{Epoch: 200, FiveHourUtil: 0.1, FiveHourReset: r5, SevenDayUtil: 0.3, SevenDayReset: r7},
			want: usageSnapshot{Epoch: 200, FiveHourUtil: 0.9, FiveHourReset: r5, SevenDayUtil: 0.3, SevenDayReset: r7},
		},
		{
			name: "missing window on one side keeps the other side's data",
			a:    usageSnapshot{Epoch: 100, FiveHourUtil: 0.1, FiveHourReset: r5, SevenDayUtil: 0.95, SevenDayReset: r7},
			b:    usageSnapshot{Epoch: 200, FiveHourUtil: 0.2, FiveHourReset: r5},
			want: usageSnapshot{Epoch: 200, FiveHourUtil: 0.2, FiveHourReset: r5, SevenDayUtil: 0.95, SevenDayReset: r7},
		},
		{
			name: "later reset is a newer window and supersedes",
			a:    usageSnapshot{Epoch: 100, FiveHourUtil: 0.95, FiveHourReset: r5},
			b:    usageSnapshot{Epoch: 200, FiveHourUtil: 0.05, FiveHourReset: r5 + 18000},
			want: usageSnapshot{Epoch: 200, FiveHourUtil: 0.05, FiveHourReset: r5 + 18000},
		},
		{
			name: "resets within tolerance are the same window",
			a:    usageSnapshot{FiveHourUtil: 0.4, FiveHourReset: r5},
			b:    usageSnapshot{FiveHourUtil: 0.3, FiveHourReset: r5 + 1},
			want: usageSnapshot{FiveHourUtil: 0.4, FiveHourReset: r5 + 1},
		},
		{
			name: "both missing stays missing",
			a:    usageSnapshot{Epoch: 1},
			b:    usageSnapshot{Epoch: 2},
			want: usageSnapshot{Epoch: 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The merge must not depend on argument order.
			for _, pair := range [][2]usageSnapshot{{tt.a, tt.b}, {tt.b, tt.a}} {
				if got := mergeSnapshots(pair[0], pair[1]); got != tt.want {
					t.Errorf("mergeSnapshots(%+v, %+v) = %+v, want %+v", pair[0], pair[1], got, tt.want)
				}
			}
		})
	}
}
