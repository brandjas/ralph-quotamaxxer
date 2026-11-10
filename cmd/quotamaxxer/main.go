// quotamaxxer — rate limit-aware wrapper for Claude Code. Runs claude behind an
// in-process reverse proxy that records rate limit headers, optionally waiting
// (guard) until burn rate is below a threshold. See help.go for subcommands.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	// Must be imported so its init runs before bubbletea's; see package doc.
	_ "github.com/brandjas/ralph-quotamaxxer/cmd/quotamaxxer/internal/notermquery"
)

const (
	headerPrefix    = "anthropic-ratelimit-unified-"
	defaultPort     = "0" // 0 = OS-assigned ephemeral port
	defaultDataDir  = ""  // resolved at runtime to ~/.claude/ralph-quotamaxxer/data
	defaultUpstream = "https://api.anthropic.com"
)

// ratelimitData is the structured JSON written to usage-proxy.json and appended to usage-history.jsonl.
type ratelimitData struct {
	Source              string            `json:"source"`
	Timestamp           string            `json:"timestamp"`
	Epoch               int64             `json:"epoch"`
	Status              string            `json:"status,omitempty"`
	RepresentativeClaim string            `json:"representative_claim,omitempty"`
	FiveHour            *windowData       `json:"five_hour,omitempty"`
	SevenDay            *windowData       `json:"seven_day,omitempty"`
	Overage             *overageData      `json:"overage,omitempty"`
	RawHeaders          map[string]string `json:"raw_headers"`
}

type windowData struct {
	Utilization float64 `json:"utilization"`
	Reset       int64   `json:"reset,omitempty"`
}

type overageData struct {
	Status string `json:"status,omitempty"`
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "monitor":
			runMonitor(os.Args[2:])
			return
		case "guard":
			runGuard(os.Args[2:])
			return
		case "proxy":
			runStandaloneProxy()
			return
		case "statusline":
			runStatusline(os.Args[2:])
			return
		case "daemon":
			os.Exit(runDaemonCommand(os.Args[2:]))
		case "help", "--help", "-h":
			runHelp()
			return
		}
	}
	os.Exit(runOrchestrator(os.Args[1:]))
}

// newProxyHandler creates the reverse proxy HTTP handler, submitting rate
// limit headers from each response to writer.
func newProxyHandler(upstreamURL *url.URL, writer *usageWriter) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(upstreamURL)

	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		req.URL.Scheme = upstreamURL.Scheme
		req.URL.Host = upstreamURL.Host
		req.Host = upstreamURL.Host
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		headers := make(map[string]string)
		for key, vals := range resp.Header {
			if strings.HasPrefix(strings.ToLower(key), headerPrefix) {
				if len(vals) > 0 {
					headers[strings.ToLower(key)] = vals[0]
				}
			}
		}
		if len(headers) > 0 {
			writer.submit(headers)
		}
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, proxyErr error) {
		log.Printf("proxy error: %s %s: %v", r.Method, r.URL.Path, proxyErr)
		w.WriteHeader(http.StatusBadGateway)
	}

	return proxy
}

// startProxyServer starts the reverse proxy in-process on the given port
// ("0" for OS-assigned ephemeral) and returns the bound address and a shutdown
// function. The caller is responsible for signal handling.
func startProxyServer(dataDir, upstream, port string) (addr string, shutdown func(), err error) {
	upstreamURL, err := url.Parse(upstream)
	if err != nil {
		return "", nil, fmt.Errorf("invalid upstream URL %q: %v", upstream, err)
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", nil, fmt.Errorf("cannot create data dir %q: %v", dataDir, err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return "", nil, fmt.Errorf("listen: %v", err)
	}

	writer := startUsageWriter(dataDir)
	handler := newProxyHandler(upstreamURL, writer)

	srv := &http.Server{Handler: handler}
	go func() {
		if serveErr := srv.Serve(ln); serveErr != http.ErrServerClosed {
			log.Printf("proxy serve error: %v", serveErr)
		}
	}()

	shutdownFn := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		// Flush the pending update: the last response before claude exits
		// carries the most recent utilization, which the next guard run needs.
		writer.close(writerFlushTimeout)
	}

	return ln.Addr().String(), shutdownFn, nil
}

// runStandaloneProxy runs the proxy as a standalone server (quotamaxxer proxy).
func runStandaloneProxy() {
	port := envOr("QUOTAMAXXER_PORT", defaultPort)
	dataDir := resolveDataDir()
	upstream := envOr("QUOTAMAXXER_UPSTREAM", defaultUpstream)

	addr, shutdown, err := startProxyServer(dataDir, upstream, port)
	if err != nil {
		log.Fatalf("quotamaxxer proxy: %v", err)
	}

	_, actualPort, _ := net.SplitHostPort(addr)
	portFile := envOr("QUOTAMAXXER_PORT_FILE", "")
	if portFile != "" {
		if err := os.WriteFile(portFile, []byte(actualPort+"\n"), 0644); err != nil {
			log.Fatalf("write port file: %v", err)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigCh
	log.Printf("received %v, shutting down...", sig)
	shutdown()
}

func parseHeaders(headers map[string]string) ratelimitData {
	now := time.Now().UTC()
	data := ratelimitData{
		Source:     "proxy",
		Timestamp:  now.Format(time.RFC3339),
		Epoch:      now.Unix(),
		RawHeaders: headers,
	}
	if v, ok := headers[headerPrefix+"status"]; ok {
		data.Status = v
	}
	if v, ok := headers[headerPrefix+"representative-claim"]; ok {
		data.RepresentativeClaim = v
	}
	data.FiveHour = parseWindowHeaders(headers, "5h")
	data.SevenDay = parseWindowHeaders(headers, "7d")
	if s, ok := headers[headerPrefix+"overage-status"]; ok {
		data.Overage = &overageData{Status: s}
	}
	return data
}

func parseWindowHeaders(headers map[string]string, prefix string) *windowData {
	u, ok := headers[headerPrefix+prefix+"-utilization"]
	if !ok {
		return nil
	}
	w := &windowData{}
	w.Utilization, _ = strconv.ParseFloat(u, 64)
	if r, ok2 := headers[headerPrefix+prefix+"-reset"]; ok2 {
		w.Reset, _ = strconv.ParseInt(r, 10, 64)
	}
	return w
}

// writerFlushTimeout bounds how long shutdown waits for pending usage writes,
// so a stuck filesystem can't hang quotamaxxer's exit.
const writerFlushTimeout = 2 * time.Second

// usageWriter persists rate limit headers off the request path. It holds at
// most one pending update; a newer update replaces an unwritten older one,
// since only the latest snapshot matters.
type usageWriter struct {
	mu     sync.Mutex
	closed bool
	ch     chan map[string]string
	done   chan struct{}
}

func startUsageWriter(dataDir string) *usageWriter {
	w := &usageWriter{
		ch:   make(chan map[string]string, 1),
		done: make(chan struct{}),
	}
	go w.run(dataDir)
	return w
}

func (w *usageWriter) run(dataDir string) {
	defer close(w.done)
	snapshotPath := filepath.Join(dataDir, "usage-proxy.json")
	historyPath := filepath.Join(dataDir, "usage-history.jsonl")
	maxHistoryBytes := maxHistoryBytesFromEnv()

	for headers := range w.ch {
		data := parseHeaders(headers)
		if err := persistAndAppendHistory(snapshotPath, historyPath, data, maxHistoryBytes); err != nil {
			log.Printf("proxy persist: %v", err)
		}
	}
}

// submit queues headers for writing, replacing any unwritten pending update.
// It never blocks on disk I/O and is a no-op after close.
func (w *usageWriter) submit(headers map[string]string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	select {
	case <-w.ch: // drop stale pending update
	default:
	}
	w.ch <- headers // cannot block: buffer is empty and we hold the lock
}

// close stops accepting updates and waits up to timeout for the pending one
// to be written.
func (w *usageWriter) close(timeout time.Duration) {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.ch)
	}
	w.mu.Unlock()

	select {
	case <-w.done:
	case <-time.After(timeout):
		log.Printf("quotamaxxer: timed out flushing usage data")
	}
}

// persistAndAppendHistory writes data as pretty JSON to snapshotPath (atomic)
// and appends a compact JSON line to historyPath (flock-guarded).
func persistAndAppendHistory(snapshotPath, historyPath string, data any, maxHistoryBytes int64) error {
	pretty, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}
	pretty = append(pretty, '\n')
	if err := atomicWriteFile(snapshotPath, pretty); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}

	compact, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("json marshal (history): %w", err)
	}
	compact = append(compact, '\n')
	lockPath := historyPath + ".lock"
	appendHistory(historyPath, lockPath, compact, maxHistoryBytes)
	return nil
}

// atomicWriteFile replaces path with data via a uniquely named temp file in
// the same directory. The unique name matters: multiple quotamaxxer and
// statusline processes write the same snapshot concurrently, and a shared
// temp name would let one writer rename another's half-written file into
// place.
func atomicWriteFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(0644)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

func appendHistory(path, lockPath string, line []byte, maxBytes int64) {
	// Acquire flock on sidecar .lock file (short timeout).
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		// Fall through to unlocked append.
		appendUnlocked(path, line)
		return
	}
	defer lockFile.Close()

	// Try flock with a 1s timeout via non-blocking + retry.
	locked := false
	for i := 0; i < 10; i++ {
		if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			locked = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !locked {
		// Timeout — fall through to unlocked append.
		appendUnlocked(path, line)
		return
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	// Rotate if over max size: keep last 50% of lines.
	// maxBytes <= 0 disables rotation.
	if maxBytes > 0 {
		if info, err := os.Stat(path); err == nil && info.Size() > maxBytes {
			rotateHistory(path)
		}
	}

	appendUnlocked(path, line)
}

func appendUnlocked(path string, line []byte) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("history append error: %v", err)
		return
	}
	defer f.Close()
	f.Write(line)
}

func rotateHistory(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	half := len(lines) / 2
	kept := strings.Join(lines[half:], "\n") + "\n"
	os.WriteFile(path, []byte(kept), 0644)
}

// resolveDataDir returns $QUOTAMAXXER_DATA_DIR if set, else the default data
// directory. Every subcommand must use this (as the --data-dir default where
// that flag exists) so all writers and readers agree on one location.
func resolveDataDir() string {
	return envOr("QUOTAMAXXER_DATA_DIR", resolveDefaultDataDir())
}

func resolveDefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".claude", "ralph-quotamaxxer", "data")
}

func maxHistoryBytesFromEnv() int64 {
	if v := os.Getenv("QUOTAMAXXER_MAX_HISTORY_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return 0
}

// orchestratorUpstream is where a headless run's proxy forwards to:
// QUOTAMAXXER_UPSTREAM if set, else the ANTHROPIC_BASE_URL the user launched
// quotamaxxer with (e.g. a corporate gateway, which UI sessions also use since
// they inherit it unchanged), else the Anthropic API. The proxy then replaces
// ANTHROPIC_BASE_URL for the child with its own address.
func orchestratorUpstream() string {
	return envOr("QUOTAMAXXER_UPSTREAM", envOr("ANTHROPIC_BASE_URL", defaultUpstream))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
