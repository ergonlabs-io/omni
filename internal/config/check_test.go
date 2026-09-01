package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"14d", 14 * 24 * time.Hour, false},
		{"10m", 10 * time.Minute, false},
		{"1d12h", 36 * time.Hour, false},
		{"90s", 90 * time.Second, false},
		{"not-a-duration", 0, true},
	}
	for _, tc := range cases {
		got, err := ParseDuration(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseDuration(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDuration(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestUnknownKeyGlobal(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeTestFile(t, GlobalConfigPath(home), `
[defaults]
mdoe = "route"
`)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := false
	for _, is := range e.Check() {
		if is.Level == LevelError && strings.Contains(is.Message, "mdoe") {
			found = true
			if !strings.Contains(is.Message, "mode") {
				t.Errorf("expected a near-miss suggestion of \"mode\", got: %s", is.Message)
			}
		}
	}
	if !found {
		t.Errorf("expected an unknown-key issue for typo'd 'mdoe', got: %+v", e.Check())
	}
}

func TestUnknownKeyAgentFile(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeTestFile(t, AgentConfigPath(home, "claude"), `
bianry = "/usr/bin/claude"
`)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := false
	for _, is := range e.Check() {
		if is.Level == LevelError && strings.Contains(is.Message, "bianry") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an unknown-key issue for 'bianry', got: %+v", e.Check())
	}
	if e.Binary.V != "" {
		t.Errorf("binary should not have been set from an unknown key, got %q", e.Binary.V)
	}
}

func TestDurationParseErrorIsNonFatal(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeTestFile(t, GlobalConfigPath(home), `
[defaults.record]
retention = "not-a-duration"
`)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	found := false
	for _, is := range e.Check() {
		if is.Path == "record.retention" && is.Level == LevelError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a LevelError issue for a bad duration, got: %+v", e.Check())
	}
	// Falls back to the built-in default rather than a zero duration.
	if e.Record.Retention.Source != builtinSource {
		t.Errorf("record.retention source = %q, want untouched built-in default", e.Record.Retention.Source)
	}
}

func TestShowFormatsProvenance(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeTestFile(t, AgentConfigPath(home, "claude"), `
mode = "route"

[[route]]
match = "claude-opus-5"
model = "claude-sonnet-5"
`)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out := e.Show()
	if !strings.Contains(out, `mode`) || !strings.Contains(out, `"route"`) {
		t.Errorf("Show() missing mode=route:\n%s", out)
	}
	if !strings.Contains(out, "claude.conf") {
		t.Errorf("Show() missing file provenance:\n%s", out)
	}
	if !strings.Contains(out, "(built-in default)") {
		t.Errorf("Show() missing built-in default provenance for untouched keys:\n%s", out)
	}
	if !strings.Contains(out, "claude-opus-5 → claude-sonnet-5") {
		t.Errorf("Show() missing route arrow formatting:\n%s", out)
	}
}
