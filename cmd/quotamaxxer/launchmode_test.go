package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNeedsProxy(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		stdinTTY  bool
		stdoutTTY bool
		want      bool
	}{
		{"interactive UI", nil, true, true, false},
		{"interactive with prompt arg", []string{"fix the bug"}, true, true, false},
		{"print short flag", []string{"-p", "hi"}, true, true, true},
		{"print long flag", []string{"--print", "hi"}, true, true, true},
		{"stdin not a TTY", nil, false, true, true},
		{"stdout not a TTY", nil, true, false, true},
		{"background", []string{"--bg", "hi"}, true, true, false},
		{"background long flag", []string{"--background", "hi"}, true, true, false},
		{"background without TTY", []string{"--bg", "hi"}, false, false, false},
		{"background wins over print", []string{"--bg", "-p", "hi"}, false, false, false},
		{"-p after -- is not a flag", []string{"--", "-p"}, true, true, false},
		{"--bg after -- is not a flag", []string{"-p", "--", "--bg"}, true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsProxy(tt.args, tt.stdinTTY, tt.stdoutTTY); got != tt.want {
				t.Errorf("needsProxy(%q, stdin=%v, stdout=%v) = %v, want %v",
					tt.args, tt.stdinTTY, tt.stdoutTTY, got, tt.want)
			}
		})
	}
}

func TestStatuslineConfigured(t *testing.T) {
	write := func(t *testing.T, path, command string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]any{"statusLine": map[string]string{"type": "command", "command": command}})
		if err := os.WriteFile(path, b, 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("nothing configured", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
		if statuslineConfigured(t.TempDir()) {
			t.Error("want false")
		}
	})
	t.Run("unrelated user statusline", func(t *testing.T) {
		cfg := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", cfg)
		write(t, filepath.Join(cfg, "settings.json"), "echo hello")
		if statuslineConfigured(t.TempDir()) {
			t.Error("want false")
		}
	})
	t.Run("user settings with persist-only", func(t *testing.T) {
		cfg := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", cfg)
		write(t, filepath.Join(cfg, "settings.json"),
			`input=$(cat); printf '%s' "$input" | ~/.claude/ralph-quotamaxxer/bin/quotamaxxer statusline --persist-only; echo hi`)
		if !statuslineConfigured(t.TempDir()) {
			t.Error("want true")
		}
	})
	t.Run("project local settings", func(t *testing.T) {
		t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
		proj := t.TempDir()
		write(t, filepath.Join(proj, ".claude", "settings.local.json"), "quotamaxxer statusline")
		if !statuslineConfigured(proj) {
			t.Error("want true")
		}
	})
	t.Run("unparseable settings", func(t *testing.T) {
		cfg := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", cfg)
		os.WriteFile(filepath.Join(cfg, "settings.json"), []byte("{not json"), 0644)
		if statuslineConfigured(t.TempDir()) {
			t.Error("want false")
		}
	})
}
