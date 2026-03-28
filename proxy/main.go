// quotamaxxer-proxy — transparent reverse proxy that extracts rate limit headers.
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
	"syscall"
	"time"
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

// writeCh receives header maps to write asynchronously.
var writeCh = make(chan map[string]string, 1)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "guard":
			runGuard(os.Args[2:])
			return
		case "proxy":
			runStandaloneProxy()
			return
		case "statusline":
			runStatusline()
			return
		case "help", "--help", "-h":
			runHelp()
			return
		}
	}
	os.Exit(runOrchestrator(os.Args[1:]))
}

// newProxyHandler creates the reverse proxy HTTP handler and starts the async writer.
func newProxyHandler(dataDir string, upstreamURL *url.URL) http.Handler {
	go asyncWriter(dataDir)

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
			select {
			case writeCh <- headers:
			default:
				select {
				case <-writeCh:
				default:
				}
				writeCh <- headers
			}
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

	handler := newProxyHandler(dataDir, upstreamURL)

	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return "", nil, fmt.Errorf("listen: %v", err)
	}

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
		close(writeCh)
	}

	return ln.Addr().String(), shutdownFn, nil
}

// runStandaloneProxy runs the proxy as a standalone server (quotamaxxer proxy).
func runStandaloneProxy() {
	port := envOr("QUOTAMAXXER_PORT", defaultPort)
	dataDir := envOr("QUOTAMAXXER_DATA_DIR", resolveDefaultDataDir())
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
	if u, ok := headers[headerPrefix+"5h-utilization"]; ok {
		w := &windowData{}
		w.Utilization, _ = strconv.ParseFloat(u, 64)
		if r, ok2 := headers[headerPrefix+"5h-reset"]; ok2 {
			w.Reset, _ = strconv.ParseInt(r, 10, 64)
		}
		data.FiveHour = w
	}
	if u, ok := headers[headerPrefix+"7d-utilization"]; ok {
		w := &windowData{}
		w.Utilization, _ = strconv.ParseFloat(u, 64)
		if r, ok2 := headers[headerPrefix+"7d-reset"]; ok2 {
			w.Reset, _ = strconv.ParseInt(r, 10, 64)
		}
		data.SevenDay = w
	}
	if s, ok := headers[headerPrefix+"overage-status"]; ok {
		data.Overage = &overageData{Status: s}
	}
	return data
}

func asyncWriter(dataDir string) {
	snapshotPath := filepath.Join(dataDir, "usage-proxy.json")
	historyPath := filepath.Join(dataDir, "usage-history.jsonl")
	lockPath := historyPath + ".lock"
	maxHistoryBytes := int64(10 * 1024 * 1024) // 10 MB

	if v := os.Getenv("QUOTAMAXXER_MAX_HISTORY_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			maxHistoryBytes = n
		}
	}

	for headers := range writeCh {
		data := parseHeaders(headers)

		// Write usage-proxy.json (pretty-printed, atomic).
		pretty, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			log.Printf("json marshal error: %v", err)
			continue
		}
		pretty = append(pretty, '\n')
		tmp := snapshotPath + ".tmp"
		if err := os.WriteFile(tmp, pretty, 0644); err != nil {
			log.Printf("write tmp error: %v", err)
			continue
		}
		if err := os.Rename(tmp, snapshotPath); err != nil {
			log.Printf("rename error: %v", err)
		}

		// Append to usage-history.jsonl (compact, flock-guarded).
		compact, err := json.Marshal(data)
		if err != nil {
			log.Printf("json marshal error (history): %v", err)
			continue
		}
		compact = append(compact, '\n')
		appendHistory(historyPath, lockPath, compact, maxHistoryBytes)
	}
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
	if info, err := os.Stat(path); err == nil && info.Size() > maxBytes {
		rotateHistory(path)
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

func resolveDefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".claude", "ralph-quotamaxxer", "data")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

