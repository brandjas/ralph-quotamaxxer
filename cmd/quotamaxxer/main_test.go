package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAtomicWriteFileConcurrent checks that concurrent writers never expose a
// partially written snapshot to readers, and leave no temp files behind.
func TestAtomicWriteFileConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage-proxy.json")

	payload := func(n int) []byte {
		// Large enough that writes aren't trivially atomic.
		b, _ := json.Marshal(map[string]string{"writer": fmt.Sprint(n), "pad": strings.Repeat("x", 64*1024)})
		return b
	}
	if err := atomicWriteFile(path, payload(-1)); err != nil {
		t.Fatal(err)
	}

	const writers, iterations = 8, 50
	var wg sync.WaitGroup
	stop := make(chan struct{})
	readErr := make(chan error, 1)

	go func() {
		for {
			select {
			case <-stop:
				close(readErr)
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				readErr <- err
				return
			}
			var v map[string]string
			if err := json.Unmarshal(data, &v); err != nil {
				readErr <- fmt.Errorf("reader saw invalid JSON (%d bytes): %v", len(data), err)
				return
			}
		}
	}()

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if err := atomicWriteFile(path, payload(w)); err != nil {
					t.Errorf("writer %d: %v", w, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(stop)
	if err := <-readErr; err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "usage-proxy.json" {
			t.Errorf("leftover file: %s", e.Name())
		}
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0644 {
		t.Errorf("snapshot mode = %v, err = %v; want 0644", info.Mode().Perm(), err)
	}
}

// TestProxyShutdownFlushesPendingWrite checks that the usage snapshot from the
// last response is on disk once shutdown returns. Previously shutdown closed
// the write channel without waiting, so exiting right after claude finished
// could drop the most recent (most important) snapshot.
func TestProxyShutdownFlushesPendingWrite(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.42")
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Reset", "1774540800")
		w.Header().Set("Anthropic-Ratelimit-Unified-Status", "allowed")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	dataDir := t.TempDir()
	addr, shutdown, err := startProxyServer(dataDir, upstream.URL, "0")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post("http://"+addr+"/v1/messages", "application/json", strings.NewReader("{}"))
	if err != nil {
		shutdown()
		t.Fatal(err)
	}
	resp.Body.Close()
	shutdown()

	snap, err := readProxyFile(filepath.Join(dataDir, "usage-proxy.json"))
	if err != nil {
		t.Fatalf("snapshot not written by the time shutdown returned: %v", err)
	}
	if snap.FiveHourUtil != 0.42 || snap.FiveHourReset != 1774540800 {
		t.Errorf("snapshot = %+v, want 5h utilization 0.42 reset 1774540800", snap)
	}
}

func TestUsageWriterKeepsLatestAndIgnoresSubmitAfterClose(t *testing.T) {
	dataDir := t.TempDir()
	w := startUsageWriter(dataDir)
	for i := 1; i <= 100; i++ {
		w.submit(map[string]string{headerPrefix + "5h-utilization": fmt.Sprintf("0.%02d", i%100)})
	}
	w.submit(map[string]string{headerPrefix + "5h-utilization": "0.77"})
	w.close(5 * time.Second)
	w.submit(map[string]string{headerPrefix + "5h-utilization": "0.99"}) // must not panic
	w.close(time.Second)                                                 // idempotent

	snap, err := readProxyFile(filepath.Join(dataDir, "usage-proxy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if snap.FiveHourUtil != 0.77 {
		t.Errorf("5h utilization = %v, want 0.77 (the last submitted update)", snap.FiveHourUtil)
	}
}
