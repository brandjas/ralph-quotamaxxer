package main

import "testing"

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
