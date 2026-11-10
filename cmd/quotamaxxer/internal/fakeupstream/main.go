// fakeupstream is a stand-in for the Anthropic API used by the e2e tests
// (test/e2e). It answers POST /v1/messages with a canned assistant reply and
// synthetic anthropic-ratelimit-unified-* headers, so Claude Code can run a
// full turn against it with no credentials, no network and no cost. Every
// request is appended to a JSON-lines log that tests assert on.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type requestRecord struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Model  string `json:"model,omitempty"`
	Stream bool   `json:"stream,omitempty"`
	// Auth is the kind of credential presented ("bearer", "api-key" or ""),
	// never the credential itself.
	Auth string `json:"auth,omitempty"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:9911", "listen address")
	logPath := flag.String("log", "", "append one JSON line per request to this file")
	reply := flag.String("reply", "ok", "assistant reply text")
	util5h := flag.Float64("util-5h", 0.12, "reported 5h utilization (0-1)")
	util7d := flag.Float64("util-7d", 0.34, "reported 7d utilization (0-1)")
	flag.Parse()

	var mu sync.Mutex
	record := func(rec requestRecord) {
		if *logPath == "" {
			return
		}
		line, _ := json.Marshal(rec)
		mu.Lock()
		defer mu.Unlock()
		f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("fakeupstream: %v", err)
			return
		}
		defer f.Close()
		f.Write(append(line, '\n'))
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		json.Unmarshal(body, &req)

		rec := requestRecord{Method: r.Method, Path: r.URL.Path, Model: req.Model, Stream: req.Stream}
		switch {
		case r.Header.Get("Authorization") != "":
			rec.Auth = "bearer"
		case r.Header.Get("X-Api-Key") != "":
			rec.Auth = "api-key"
		}
		record(rec)

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages":
			setRateLimitHeaders(w.Header(), *util5h, *util7d)
			if req.Stream {
				streamMessage(w, req.Model, *reply)
			} else {
				json.NewEncoder(w).Encode(message(req.Model, *reply, "end_turn"))
			}
		case r.Method == http.MethodPost && r.URL.Path == "/v1/messages/count_tokens":
			fmt.Fprint(w, `{"input_tokens":1}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"type":"error","error":{"type":"not_found_error","message":"fakeupstream: not implemented"}}`)
		}
	})

	log.Printf("fakeupstream: listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func setRateLimitHeaders(h http.Header, util5h, util7d float64) {
	now := time.Now().Unix()
	reset5h := strconv.FormatInt(now+3*3600, 10)
	const p = "anthropic-ratelimit-unified-"
	h.Set(p+"status", "allowed")
	h.Set(p+"representative-claim", "five_hour")
	h.Set(p+"reset", reset5h)
	h.Set(p+"fallback-percentage", "0.5")
	h.Set(p+"overage-status", "rejected")
	h.Set(p+"5h-status", "allowed")
	h.Set(p+"5h-utilization", strconv.FormatFloat(util5h, 'f', -1, 64))
	h.Set(p+"5h-reset", reset5h)
	h.Set(p+"7d-status", "allowed")
	h.Set(p+"7d-utilization", strconv.FormatFloat(util7d, 'f', -1, 64))
	h.Set(p+"7d-reset", strconv.FormatInt(now+3*86400, 10))
}

func message(model, text string, stopReason any) map[string]any {
	content := []any{}
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	return map[string]any{
		"id": "msg_fake", "type": "message", "role": "assistant", "model": model,
		"content": content, "stop_reason": stopReason, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 3, "output_tokens": 1},
	}
}

func streamMessage(w http.ResponseWriter, model, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	send := func(event string, data map[string]any) {
		data["type"] = event
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	send("message_start", map[string]any{"message": message(model, "", nil)})
	send("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	send("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": text}})
	send("content_block_stop", map[string]any{"index": 0})
	send("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 1},
	})
	send("message_stop", map[string]any{})
}
