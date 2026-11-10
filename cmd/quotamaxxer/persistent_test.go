package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersistentKeyIsCreatedOnceAndPrivate(t *testing.T) {
	dir := t.TempDir()
	k1, err := loadOrCreatePersistentKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := loadOrCreatePersistentKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(k1) != string(k2) || len(k1) != 32 {
		t.Fatalf("key not stable: %x vs %x", k1, k2)
	}
	info, err := os.Stat(filepath.Join(dir, persistentKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("key file mode = %o, want 600", perm)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestPersistentBaseURL(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	if got := persistentBaseURL("1234", key, defaultUpstream); got != "http://127.0.0.1:1234" {
		t.Errorf("default upstream: got %q", got)
	}
	if got := persistentBaseURL("1234", key, defaultUpstream+"/"); got != "http://127.0.0.1:1234" {
		t.Errorf("default upstream with slash: got %q", got)
	}
	got := persistentBaseURL("1234", key, "https://gateway.example/anthropic")
	if !strings.HasPrefix(got, "http://127.0.0.1:1234"+upstreamPrefix) {
		t.Errorf("custom upstream: got %q", got)
	}
}

func TestIsPersistentProxyURL(t *testing.T) {
	for url, want := range map[string]bool{
		"http://127.0.0.1:28429":                    true,
		"http://localhost:28429/_quotamaxxer/u/a/b": true,
		"http://127.0.0.1:9999":                     false,
		"https://api.anthropic.com":                 false,
		"http://gateway.example:28429":              false,
		"":                                          false,
	} {
		if got := isPersistentProxyURL(url, "28429"); got != want {
			t.Errorf("isPersistentProxyURL(%q) = %v, want %v", url, got, want)
		}
	}
}

// startTestDaemon serves the persistent handler in-process on a random port.
func startTestDaemon(t *testing.T, key []byte, stop func()) (port string, dataDir string) {
	t.Helper()
	dataDir = t.TempDir()
	writer := startUsageWriter(dataDir)
	srv := httptest.NewServer(newPersistentHandler(key, writer, "testbuild", stop))
	t.Cleanup(func() {
		srv.Close()
		writer.close(time.Second)
	})
	return srv.URL[strings.LastIndex(srv.URL, ":")+1:], dataDir
}

func TestPersistentHandlerRoutesSignedUpstream(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set(headerPrefix+"5h-utilization", "0.5")
		w.Header().Set(headerPrefix+"5h-reset", "1900000000")
		io.WriteString(w, "from upstream")
	}))
	defer upstream.Close()

	key := []byte("0123456789abcdef0123456789abcdef")
	port, dataDir := startTestDaemon(t, key, func() {})

	// The upstream has a path of its own, as a gateway might.
	base := persistentBaseURL(port, key, upstream.URL+"/gw")
	resp, err := http.Get(base + "/v1/messages")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "from upstream" || gotPath != "/gw/v1/messages" {
		t.Fatalf("body %q, upstream path %q", body, gotPath)
	}

	// Usage from that upstream is recorded.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dataDir, "usage-proxy.json")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("usage-proxy.json was not written")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A prefix that wasn't signed with the key is refused, not forwarded.
	forged := persistentBaseURL(port, []byte("another key, 32 bytes long......"), upstream.URL+"/gw")
	resp, err = http.Get(forged + "/v1/messages")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("forged prefix: status %d, want 403", resp.StatusCode)
	}
}

func TestPersistentProbeAndShutdown(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	stopped := make(chan struct{}, 1)
	port, _ := startTestDaemon(t, key, func() { stopped <- struct{}{} })

	state, health := probePersistent(port, key)
	if state != persistentOurs || health.Build != "testbuild" || health.PID != os.Getpid() {
		t.Fatalf("probe with the right key: state %v, health %+v", state, health)
	}

	// Without the key, the daemon is indistinguishable from a stranger and
	// cannot be stopped.
	other := []byte("another key, 32 bytes long......")
	if state, _ := probePersistent(port, other); state != persistentForeign {
		t.Errorf("probe with the wrong key: state %v, want foreign", state)
	}
	if err := requestPersistentShutdown(port, other); err == nil {
		t.Error("shutdown with the wrong key succeeded")
	}
	select {
	case <-stopped:
		t.Fatal("stop was called for an unauthenticated request")
	default:
	}

	if err := requestPersistentShutdown(port, key); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop was not called")
	}
}

func TestPersistentProbeStates(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")

	// Something else on the port: any HTTP server that isn't our daemon.
	stranger := httptest.NewServer(http.NotFoundHandler())
	strangerPort := stranger.URL[strings.LastIndex(stranger.URL, ":")+1:]
	if state, _ := probePersistent(strangerPort, key); state != persistentForeign {
		t.Errorf("stranger: state %v, want foreign", state)
	}

	// Nothing on the port (the one the stranger just released).
	stranger.Close()
	if state, _ := probePersistent(strangerPort, key); state != persistentAbsent {
		t.Errorf("closed port: state %v, want absent", state)
	}
}
