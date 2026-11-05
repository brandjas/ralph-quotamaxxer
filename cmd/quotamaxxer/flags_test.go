package main

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseOrchestratorArgsPassthrough(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		claude []string
	}{
		{"empty", nil, []string{}},
		{"only separator", []string{"--"}, []string{}},
		{"claude flag without separator", []string{"-p", "hello"}, []string{"-p", "hello"}},
		{"claude long flag", []string{"--resume"}, []string{"--resume"}},
		{"positional prompt", []string{"fix the bug"}, []string{"fix the bug"}},
		{"own flags then claude flags", []string{"--threshold-5h", "0.8", "--quiet", "-p", "hi"}, []string{"-p", "hi"}},
		{"own flag with equals", []string{"--threshold-7d=0.9", "-p", "hi"}, []string{"-p", "hi"}},
		{"own flags then separator", []string{"--quiet", "--", "-p", "hi"}, []string{"-p", "hi"}},
		{"double separator", []string{"--", "--", "hi"}, []string{"--", "hi"}},
		{"later quotamaxxer-looking flags go to claude", []string{"-p", "hi", "--quiet"}, []string{"-p", "hi", "--quiet"}},
		{"separator after claude args kept", []string{"-p", "--", "x"}, []string{"-p", "--", "x"}},
		{"single dash is positional", []string{"-"}, []string{"-"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseOrchestratorArgs(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !(len(cfg.ClaudeArgs) == 0 && len(tt.claude) == 0) && !reflect.DeepEqual(cfg.ClaudeArgs, tt.claude) {
				t.Errorf("ClaudeArgs = %q, want %q", cfg.ClaudeArgs, tt.claude)
			}
		})
	}
}

func TestParseOrchestratorArgsOwnFlags(t *testing.T) {
	cfg, err := parseOrchestratorArgs([]string{
		"--threshold-5h", "0.8", "-threshold-7d=0.9", "--wait-timeout", "30m",
		"--source", "both", "--quiet", "--claude-command", "echo", "-p", "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Threshold5h != 0.8 || cfg.Threshold7d != 0.9 || cfg.WaitTimeout != 30*time.Minute ||
		cfg.Source != "both" || !cfg.Quiet || cfg.ClaudeCmd != "echo" {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestParseOrchestratorArgsErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing value before separator", []string{"--threshold-5h", "--"}, "invalid value"},
		{"missing value at end", []string{"--threshold-5h"}, "needs an argument"},
		{"bad source", []string{"--source", "nope"}, "invalid --source"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseOrchestratorArgs(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}
