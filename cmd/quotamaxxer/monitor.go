package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/NimbleMarkets/ntcharts/sparkline"
)

type tickMsg time.Time

const yAxisWidth = 6 // "100% " + "│"

type monitorModel struct {
	dataDir string
	source  string

	width  int
	height int

	spark5h sparkline.Model
	spark7d sparkline.Model

	latest     usageSnapshot
	lastUpdate time.Time
	err        error
}

// historyRecord is a union struct for parsing both proxy and statusline JSONL records.
type historyRecord struct {
	Source   string      `json:"source"`
	Epoch    int64       `json:"epoch"`
	FiveHour *windowData `json:"five_hour"`
	SevenDay *windowData `json:"seven_day"`
	Timestamp struct {
		Epoch int64 `json:"epoch"`
	} `json:"timestamp"`
	RateLimits struct {
		FiveHour struct {
			UsedPct float64 `json:"used_pct"`
		} `json:"five_hour"`
		SevenDay struct {
			UsedPct float64 `json:"used_pct"`
		} `json:"seven_day"`
	} `json:"rate_limits"`
}

func (r historyRecord) epoch() int64 {
	if r.Epoch > 0 {
		return r.Epoch
	}
	return r.Timestamp.Epoch
}

func (r historyRecord) fiveHourPct() float64 {
	if r.Source == "proxy" && r.FiveHour != nil {
		return r.FiveHour.Utilization * 100
	}
	return r.RateLimits.FiveHour.UsedPct
}

func (r historyRecord) sevenDayPct() float64 {
	if r.Source == "proxy" && r.SevenDay != nil {
		return r.SevenDay.Utilization * 100
	}
	return r.RateLimits.SevenDay.UsedPct
}

func loadHistory(dataDir string) []historyRecord {
	path := filepath.Join(dataDir, "usage-history.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var records []historyRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var rec historyRecord
		if json.Unmarshal(scanner.Bytes(), &rec) == nil && rec.epoch() > 0 {
			records = append(records, rec)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].epoch() < records[j].epoch()
	})
	return records
}

// bucketHistory samples the history at numBuckets evenly-spaced time points
// across [now-windowSec, now]. For each column, it finds the most recent
// record at or before that time (binary search). This avoids all bucket-
// boundary artifacts: shifting 'now' by 1 second shifts each column's
// sample time by 1 second, and the looked-up value only changes when a
// record epoch actually crosses that sample time.
func bucketHistory(records []historyRecord, windowSec int64, numBuckets int, now int64, getValue func(historyRecord) float64) []float64 {
	start := now - windowSec
	buckets := make([]float64, numBuckets)

	// Filter to in-window records (already sorted ascending by epoch).
	var filtered []historyRecord
	for _, r := range records {
		e := r.epoch()
		if e >= start && e <= now {
			filtered = append(filtered, r)
		}
	}

	if len(filtered) == 0 {
		return buckets
	}

	// For each column, sample the most recent record at or before the
	// column's time point using binary search.
	step := float64(windowSec) / float64(numBuckets)
	for i := 0; i < numBuckets; i++ {
		t := start + int64(float64(i)*step+step)
		// Find rightmost record with epoch <= t.
		lo := sort.Search(len(filtered), func(i int) bool {
			return filtered[i].epoch() > t
		})
		if lo > 0 {
			buckets[i] = getValue(filtered[lo-1])
		}
	}

	return buckets
}

func sparklineWidth(termWidth int) int {
	w := termWidth - yAxisWidth - 1 // margin
	if w < 10 {
		w = 10
	}
	return w
}

func chartHeight(termHeight int) int {
	// header(3) + footer(2) = 5 fixed rows
	// 2 charts, each with 1 label + 1 time-axis row = 2 overhead per chart
	chartArea := termHeight - 5 - 4 // 4 = 2 overhead × 2 charts
	if chartArea < 4 {
		chartArea = 4
	}
	h := chartArea / 2
	if h < 2 {
		h = 2
	}
	return h
}

func buildSparkline(records []historyRecord, windowSec int64, sw, sh int, now int64, getValue func(historyRecord) float64) sparkline.Model {
	s := sparkline.New(sw, sh, sparkline.WithMaxValue(100))
	s.PushAll(bucketHistory(records, windowSec, sw, now, getValue))
	s.Draw()
	return s
}

func (m *monitorModel) rebuildCharts() {
	records := loadHistory(m.dataDir)
	sw := sparklineWidth(m.width)
	sh := chartHeight(m.height)
	now := time.Now().Unix()

	m.spark5h = buildSparkline(records, window5hSeconds, sw, sh, now, historyRecord.fiveHourPct)
	m.spark7d = buildSparkline(records, window7dSeconds, sw, sh, now, historyRecord.sevenDayPct)
}

func newMonitorModel(dataDir, source string) monitorModel {
	m := monitorModel{
		dataDir: dataDir,
		source:  source,
		width:   80,
		height:  24,
	}

	m.rebuildCharts()

	snap, err := readLatestUsage(dataDir, source)
	if err == nil {
		m.latest = snap
	}
	m.lastUpdate = time.Now()

	return m
}

func (m monitorModel) Init() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m monitorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		k := msg.String()
		if k == "q" || k == "ctrl+c" {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.rebuildCharts()
	case tickMsg:
		snap, err := readLatestUsage(m.dataDir, m.source)
		if err == nil {
			m.latest = snap
		}
		m.err = err
		m.lastUpdate = time.Now()
		m.rebuildCharts()
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}
	return m, nil
}

var (
	styleGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleRed    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleDim    = lipgloss.NewStyle().Faint(true)
	styleBold   = lipgloss.NewStyle().Bold(true)
)

func lipglossColorForPct(pct float64) lipgloss.Style {
	if pct > 80 {
		return styleRed
	}
	if pct > 50 {
		return styleYellow
	}
	return styleGreen
}

// renderYAxis returns a multi-line Y-axis label column matching the given height.
func renderYAxis(h int) string {
	lines := make([]string, h)
	for i := 0; i < h; i++ {
		switch {
		case i == 0:
			lines[i] = "100%│"
		case i == h-1:
			lines[i] = "  0%│"
		case h >= 5 && i == h/4:
			lines[i] = " 75%│"
		case h >= 3 && i == h/2:
			lines[i] = " 50%│"
		case h >= 5 && i == 3*h/4:
			lines[i] = " 25%│"
		default:
			lines[i] = "    │"
		}
	}
	return strings.Join(lines, "\n")
}

// renderTimeAxis returns a single-line time axis label for the given window.
func renderTimeAxis(chartWidth int, windowSec int64) string {
	type label struct {
		text string
		age  int64 // seconds before now
	}

	var labels []label
	if windowSec == window5hSeconds {
		labels = []label{
			{"-5h", window5hSeconds},
			{"-4h", 4 * 3600},
			{"-3h", 3 * 3600},
			{"-2h", 2 * 3600},
			{"-1h", 3600},
			{"now", 0},
		}
	} else {
		labels = []label{
			{"-7d", window7dSeconds},
			{"-6d", 6 * 86400},
			{"-5d", 5 * 86400},
			{"-4d", 4 * 86400},
			{"-3d", 3 * 86400},
			{"-2d", 2 * 86400},
			{"-1d", 86400},
			{"now", 0},
		}
	}

	line := make([]byte, chartWidth)
	for i := range line {
		line[i] = ' '
	}

	for _, l := range labels {
		// Position: fraction of window elapsed at this label
		frac := float64(windowSec-l.age) / float64(windowSec)
		pos := int(frac * float64(chartWidth-1))
		if pos < 0 {
			pos = 0
		}
		if pos+len(l.text) > chartWidth {
			pos = chartWidth - len(l.text)
		}
		copy(line[pos:], l.text)
	}

	return styleDim.Render(string(line))
}

func (m monitorModel) View() string {
	// Header.
	title := styleBold.Render("quotamaxxer monitor")

	var statsLine string
	if m.latest.Epoch > 0 {
		fhPct := m.latest.FiveHourUtil * 100
		sdPct := m.latest.SevenDayUtil * 100

		fhBurn := computeBurnRatio(m.latest.FiveHourUtil, m.latest.FiveHourReset, window5hSeconds)
		sdBurn := computeBurnRatio(m.latest.SevenDayUtil, m.latest.SevenDayReset, window7dSeconds)

		fhReset := ""
		if m.latest.FiveHourReset > 0 {
			secs := float64(m.latest.FiveHourReset - time.Now().Unix())
			if secs > 0 {
				fhReset = fmt.Sprintf(" resets %s", formatDuration(secs))
			}
		}

		fhStyle := lipglossColorForPct(fhPct)
		sdStyle := lipglossColorForPct(sdPct)

		statsLine = fmt.Sprintf("%s | %s",
			fhStyle.Render(fmt.Sprintf("5h: %.0f%% burn %.2f%s", fhPct, fhBurn, fhReset)),
			sdStyle.Render(fmt.Sprintf("7d: %.0f%% burn %.2f", sdPct, sdBurn)),
		)
	} else if m.err != nil {
		statsLine = styleDim.Render("no usage data found")
	} else {
		statsLine = styleDim.Render("waiting for data...")
	}

	header := lipgloss.JoinVertical(lipgloss.Left, title, statsLine, "")

	sh := chartHeight(m.height)
	sw := sparklineWidth(m.width)
	yAxis := renderYAxis(sh)
	padding := strings.Repeat(" ", yAxisWidth)

	renderChart := func(title string, s sparkline.Model, windowSec int64) string {
		return lipgloss.JoinVertical(lipgloss.Left,
			styleBold.Render(title),
			lipgloss.JoinHorizontal(lipgloss.Top, yAxis, s.View()),
			padding+renderTimeAxis(sw, windowSec),
		)
	}

	chart5h := renderChart("5-Hour Utilization", m.spark5h, window5hSeconds)
	chart7d := renderChart("7-Day Utilization", m.spark7d, window7dSeconds)

	// Footer.
	ago := time.Since(m.lastUpdate).Truncate(time.Second)
	footer := styleDim.Render(fmt.Sprintf("q quit | updated %s ago", ago))

	return lipgloss.JoinVertical(lipgloss.Left, header, chart5h, chart7d, footer)
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func runMonitor(args []string) {
	fs := flag.NewFlagSet("monitor", flag.ExitOnError)
	dataDir := fs.String("data-dir", resolveDefaultDataDir(), "data directory")
	source := fs.String("source", "proxy", "data source: proxy (default), both, statusline")
	fs.Parse(args)

	if err := validateSource(*source); err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer monitor: %v\n", err)
		os.Exit(1)
	}

	model := newMonitorModel(*dataDir, *source)
	var opts []tea.ProgramOption
	if isTerminal() {
		opts = append(opts, tea.WithAltScreen())
	} else {
		opts = append(opts, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	}
	p := tea.NewProgram(model, opts...)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "quotamaxxer monitor: %v\n", err)
		os.Exit(1)
	}
}
