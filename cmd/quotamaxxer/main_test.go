package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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
