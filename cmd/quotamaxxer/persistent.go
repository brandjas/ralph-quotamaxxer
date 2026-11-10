package main

// Persistent proxy (opt-in, QUOTAMAXXER_PERSISTENT=1 or --persistent).
//
// By default the proxy only lives as long as one headless run, and UI
// sessions are tracked through the statusline, because Claude Code hands
// ANTHROPIC_BASE_URL on to background agents that outlive quotamaxxer. The
// persistent proxy removes that limit: a detached `quotamaxxer daemon run`
// listens on a fixed loopback port, so the URL stays valid after the launcher
// exits and every kind of session can be proxied.
//
// Nothing is installed: the daemon is this same binary, started on demand.
// The fixed port doubles as the lock (bind is atomic), so there is no pidfile
// to go stale in a ~/.claude that containers share without sharing a network.
// A dead daemon is restarted by the next launch, and by the statusline of any
// session that still depends on it.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultPersistentPort = "28429"
	ctlPrefix             = "/_quotamaxxer/"
	upstreamPrefix        = ctlPrefix + "u/"

	// persistentSessionEnv is set (to the daemon's port) in the environment
	// of sessions launched through the persistent proxy.
	persistentSessionEnv = "QUOTAMAXXER_PROXIED_PORT"

	persistentKeyFile = "persistent-proxy.key"
	persistentLogFile = "persistent-proxy.log"
	persistentLogMax  = 1 << 20

	// persistentDrainTimeout bounds how long a daemon that was told to stop
	// keeps serving its in-flight (streaming) requests.
	persistentDrainTimeout = time.Hour
)

// Timings, as vars so tests can shorten them.
var (
	persistentProbeTimeout = 500 * time.Millisecond
	persistentStartTimeout = 5 * time.Second
	persistentBindTimeout  = 3 * time.Second
)

func persistentEnabled() bool {
	switch strings.ToLower(os.Getenv("QUOTAMAXXER_PERSISTENT")) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func persistentPort() string {
	return envOr("QUOTAMAXXER_PERSISTENT_PORT", defaultPersistentPort)
}

// loadOrCreatePersistentKey returns the secret shared by this user's
// launchers and daemons for dataDir, creating it on first use. It is what
// lets a launcher tell its own daemon from anything else that happens to
// listen on the port (another user's daemon, or a squatter that would
// otherwise be handed the session's credentials).
func loadOrCreatePersistentKey(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, persistentKeyFile)
	if key, err := readPersistentKey(path); err == nil {
		return key, nil
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	// Write to a private temp file and hard-link it into place: link fails
	// if another process created the key first, in which case theirs wins.
	tmp, err := os.CreateTemp(dataDir, persistentKeyFile+".*.tmp")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.WriteString(hex.EncodeToString(raw) + "\n")
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if err := os.Link(tmp.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return readPersistentKey(path)
}

func readPersistentKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) < 16 {
		return nil, fmt.Errorf("%s is not a valid key", path)
	}
	return key, nil
}

func persistentProof(key []byte, msg string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

// selfBuild identifies this executable by content, so a launcher can tell
// that the running daemon is from a different build (e.g. after an upgrade).
var selfBuild = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	f, err := os.Open(exe)
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
})

// persistentBaseURL is the ANTHROPIC_BASE_URL a session gets. The daemon
// forwards plain paths to the Anthropic API; any other upstream (a gateway
// the user launched with) is carried in a signed path prefix, since one
// daemon serves sessions that may have been started with different upstreams.
func persistentBaseURL(port string, key []byte, upstream string) string {
	base := "http://127.0.0.1:" + port
	upstream = strings.TrimRight(upstream, "/")
	if upstream == defaultUpstream {
		return base
	}
	return base + upstreamPrefix + persistentProof(key, "upstream:"+upstream)[:32] + "/" +
		base64.RawURLEncoding.EncodeToString([]byte(upstream))
}

// isPersistentProxyURL reports whether rawURL already points at the
// persistent proxy on port, as it does when quotamaxxer is launched from
// inside a session that is itself proxied.
func isPersistentProxyURL(rawURL, port string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Port() != port {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// --- daemon ---

type persistentHealth struct {
	App   string `json:"app"`
	Build string `json:"build"`
	PID   int    `json:"pid"`
	Proof string `json:"proof"`
}

// newPersistentHandler serves the control endpoints and proxies everything
// else. stop is called (once) when an authenticated shutdown is requested.
func newPersistentHandler(key []byte, writer *usageWriter, build string, stop func()) http.Handler {
	defaultURL, _ := url.Parse(defaultUpstream)
	defaultProxy := newProxyHandler(defaultURL, writer)

	var mu sync.Mutex
	proxies := map[string]http.Handler{}
	proxyFor := func(upstream string) (http.Handler, error) {
		mu.Lock()
		defer mu.Unlock()
		if h, ok := proxies[upstream]; ok {
			return h, nil
		}
		u, err := url.Parse(upstream)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("invalid upstream %q", upstream)
		}
		h := newProxyHandler(u, writer)
		proxies[upstream] = h
		return h, nil
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case !strings.HasPrefix(path, ctlPrefix):
			defaultProxy.ServeHTTP(w, r)

		case strings.HasPrefix(path, upstreamPrefix):
			// /_quotamaxxer/u/<mac>/<base64url upstream>/<api path>
			parts := strings.SplitN(strings.TrimPrefix(path, upstreamPrefix), "/", 3)
			if len(parts) < 2 {
				http.Error(w, "bad upstream prefix", http.StatusBadRequest)
				return
			}
			raw, err := base64.RawURLEncoding.DecodeString(parts[1])
			upstream := string(raw)
			if err != nil || !hmac.Equal([]byte(parts[0]), []byte(persistentProof(key, "upstream:"+upstream)[:32])) {
				http.Error(w, "bad upstream prefix", http.StatusForbidden)
				return
			}
			h, err := proxyFor(upstream)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			if len(parts) == 3 {
				r2.URL.Path += parts[2]
			}
			r2.URL.RawPath = ""
			h.ServeHTTP(w, r2)

		case path == ctlPrefix+"health":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(persistentHealth{
				App:   "quotamaxxer",
				Build: build,
				PID:   os.Getpid(),
				Proof: persistentProof(key, "health:"+r.URL.Query().Get("nonce")),
			})

		case path == ctlPrefix+"shutdown" && r.Method == http.MethodPost:
			if !hmac.Equal([]byte(r.Header.Get("X-Quotamaxxer-Proof")), []byte(persistentProof(key, "shutdown"))) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			stop()

		default:
			http.NotFound(w, r)
		}
	})
}

// runPersistentDaemon is `quotamaxxer daemon run`: the detached proxy
// process. It runs until told to stop, then stops accepting connections
// right away (freeing the port for a successor) and drains requests that are
// still in flight.
func runPersistentDaemon() int {
	log.SetPrefix(fmt.Sprintf("[%d] ", os.Getpid()))
	dataDir := resolveDataDir()
	port := persistentPort()
	build := selfBuild() // before anything replaces the executable on disk

	key, err := loadOrCreatePersistentKey(dataDir)
	if err != nil {
		log.Printf("persistent proxy: key: %v", err)
		return 1
	}

	// A daemon being replaced releases the port only once it handles the
	// shutdown request, so retry briefly instead of giving up at once.
	var ln net.Listener
	deadline := time.Now().Add(persistentBindTimeout)
	for {
		ln, err = net.Listen("tcp", "127.0.0.1:"+port)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EADDRINUSE) || time.Now().After(deadline) {
			// Usually another launch won the race to start the daemon.
			log.Printf("persistent proxy: not starting: %v", err)
			return 1
		}
		time.Sleep(50 * time.Millisecond)
	}

	writer := startUsageWriter(dataDir)
	stopCh := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(stopCh) }) }

	srv := &http.Server{Handler: newPersistentHandler(key, writer, build, stop)}
	go func() {
		if err := srv.Serve(ln); err != http.ErrServerClosed {
			log.Printf("persistent proxy: serve: %v", err)
			stop()
		}
	}()
	log.Printf("persistent proxy: listening on %s (build %s, data dir %s)", ln.Addr(), build, dataDir)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	select {
	case sig := <-sigCh:
		log.Printf("persistent proxy: received %v, draining", sig)
	case <-stopCh:
		log.Printf("persistent proxy: stop requested, draining")
	}
	signal.Stop(sigCh) // a second signal ends the drain immediately

	ctx, cancel := context.WithTimeout(context.Background(), persistentDrainTimeout)
	defer cancel()
	srv.Shutdown(ctx)
	writer.close(writerFlushTimeout)
	log.Printf("persistent proxy: exited")
	return 0
}

// --- client side ---

type persistentState int

const (
	persistentAbsent  persistentState = iota // nothing listens on the port
	persistentOurs                           // our daemon (proved knowledge of the key)
	persistentForeign                        // something else holds the port
)

var persistentHTTP = &http.Client{
	// Never route loopback control requests through a configured proxy.
	Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
}

func probePersistent(port string, key []byte) (persistentState, persistentHealth) {
	var health persistentHealth
	nonceRaw := make([]byte, 16)
	rand.Read(nonceRaw)
	nonce := hex.EncodeToString(nonceRaw)

	ctx, cancel := context.WithTimeout(context.Background(), persistentProbeTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://127.0.0.1:"+port+ctlPrefix+"health?nonce="+nonce, nil)
	resp, err := persistentHTTP.Do(req)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return persistentAbsent, health
		}
		return persistentForeign, health
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK ||
		json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&health) != nil ||
		!hmac.Equal([]byte(health.Proof), []byte(persistentProof(key, "health:"+nonce))) {
		return persistentForeign, health
	}
	return persistentOurs, health
}

func requestPersistentShutdown(port string, key []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), persistentProbeTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://127.0.0.1:"+port+ctlPrefix+"shutdown", nil)
	req.Header.Set("X-Quotamaxxer-Proof", persistentProof(key, "shutdown"))
	resp, err := persistentHTTP.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("shutdown request: %s", resp.Status)
	}
	return nil
}

// spawnPersistentDaemon starts `quotamaxxer daemon run` in its own session,
// so it survives the launcher, the terminal and Ctrl-C. It does not wait for
// the daemon to come up.
func spawnPersistentDaemon(dataDir, port string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	logPath := filepath.Join(dataDir, persistentLogFile)
	flags := os.O_CREATE | os.O_APPEND | os.O_WRONLY
	if info, err := os.Stat(logPath); err == nil && info.Size() > persistentLogMax {
		flags |= os.O_TRUNC
	}
	logFile, err := os.OpenFile(logPath, flags, 0644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	cmd := exec.Command(exe, "daemon", "run")
	cmd.Dir = "/"
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		"QUOTAMAXXER_DATA_DIR="+dataDir,
		"QUOTAMAXXER_PERSISTENT_PORT="+port,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap it if it exits while we are still running
	return nil
}

// ensurePersistentProxy makes sure this user's daemon for dataDir is running
// on port, starting or replacing it as needed. On error the caller falls
// back to the default (non-persistent) behaviour.
func ensurePersistentProxy(dataDir, port string) ([]byte, error) {
	key, err := loadOrCreatePersistentKey(dataDir)
	if err != nil {
		return nil, fmt.Errorf("key: %w", err)
	}

	state, health := probePersistent(port, key)
	replacing := 0
	switch {
	case state == persistentOurs && health.Build == selfBuild():
		return key, nil
	case state == persistentForeign:
		return nil, fmt.Errorf("port %s is in use by something other than this data dir's quotamaxxer proxy "+
			"(set QUOTAMAXXER_PERSISTENT_PORT to use another port)", port)
	case state == persistentOurs:
		// A daemon from another build (typically: quotamaxxer was upgraded).
		// It frees the port at once and finishes its in-flight requests.
		replacing = health.PID
		if err := requestPersistentShutdown(port, key); err != nil {
			return key, nil // still a working proxy; keep using it
		}
	}

	if err := spawnPersistentDaemon(dataDir, port); err != nil {
		return nil, fmt.Errorf("start daemon: %w", err)
	}
	deadline := time.Now().Add(persistentStartTimeout)
	for {
		state, health = probePersistent(port, key)
		if state == persistentOurs && health.PID != replacing {
			return key, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("daemon did not come up on port %s (see %s)",
				port, filepath.Join(dataDir, persistentLogFile))
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// revivePersistentProxy is the statusline's cheap self-healing check: if
// nothing listens on the port, start the daemon and return without waiting.
// A session that was launched through the persistent proxy keeps its URL for
// life, so the session itself is what brings a dead daemon back.
func revivePersistentProxy(dataDir, port string) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 200*time.Millisecond)
	if err == nil {
		conn.Close()
		return
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		spawnPersistentDaemon(dataDir, port)
	}
}

// runDaemonCommand is `quotamaxxer daemon <start|stop|status|run>`.
func runDaemonCommand(args []string) int {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	dataDir := resolveDataDir()
	port := persistentPort()

	switch sub {
	case "run":
		return runPersistentDaemon()

	case "start":
		if _, err := ensurePersistentProxy(dataDir, port); err != nil {
			fmt.Fprintln(os.Stderr, "quotamaxxer:", err)
			return 1
		}
		fmt.Printf("persistent proxy running on 127.0.0.1:%s\n", port)
		return 0

	case "stop", "status":
		key, err := loadOrCreatePersistentKey(dataDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "quotamaxxer:", err)
			return 1
		}
		state, health := probePersistent(port, key)
		switch state {
		case persistentAbsent:
			fmt.Printf("persistent proxy is not running (port %s)\n", port)
			if sub == "status" {
				return 3
			}
			return 0
		case persistentForeign:
			fmt.Fprintf(os.Stderr, "quotamaxxer: port %s is in use by something other than this data dir's quotamaxxer proxy\n", port)
			return 1
		}
		if sub == "status" {
			note := ""
			if health.Build != selfBuild() {
				note = " (a different build; the next launch replaces it)"
			}
			fmt.Printf("persistent proxy running on 127.0.0.1:%s, pid %d%s\n", port, health.PID, note)
			return 0
		}
		if err := requestPersistentShutdown(port, key); err != nil {
			fmt.Fprintln(os.Stderr, "quotamaxxer:", err)
			return 1
		}
		for deadline := time.Now().Add(persistentStartTimeout); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
			if state, _ := probePersistent(port, key); state == persistentAbsent {
				break
			}
		}
		fmt.Println("persistent proxy stopped (requests in flight are allowed to finish)")
		return 0

	default:
		fmt.Fprintf(os.Stderr, "quotamaxxer: unknown daemon command %q (expected start, stop or status)\n", sub)
		return 2
	}
}
