package config

import (
	"strings"
	"testing"
)

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

func TestUnknownKeyAgentSection(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeAgentSection(t, home, "claude", `
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

// TestBadValueIsNonFatal checks that an invalid value is a LevelError that
// `omni config check` reports, not a Fatal that refuses to load — and that
// the key keeps its lower-layer value rather than silently becoming a zero.
func TestBadValueIsNonFatal(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeTestFile(t, GlobalConfigPath(home), `
[defaults]
mode = "not-a-mode"
`)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	found := false
	for _, is := range e.Check() {
		if is.Path == "mode" && is.Level == LevelError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a LevelError issue for a bad mode, got: %+v", e.Check())
	}
	// Falls back to the built-in default rather than an empty mode.
	if e.Mode.V != ModeRecord || e.Mode.Source != builtinSource {
		t.Errorf("mode = %+v, want untouched built-in default", e.Mode)
	}
}

func TestShowFormatsProvenance(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeAgentSection(t, home, "claude", `
mode = "off"

[[route]]
match = "claude-opus-5"
model = "claude-sonnet-5"
`)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out := e.Show()
	if !strings.Contains(out, `mode`) || !strings.Contains(out, `"off"`) {
		t.Errorf("Show() missing mode=off:\n%s", out)
	}
	if !strings.Contains(out, "omni.conf") {
		t.Errorf("Show() missing file provenance:\n%s", out)
	}
	if !strings.Contains(out, "(built-in default)") {
		t.Errorf("Show() missing built-in default provenance for untouched keys:\n%s", out)
	}
	if !strings.Contains(out, "claude-opus-5 → claude-sonnet-5") {
		t.Errorf("Show() missing route arrow formatting:\n%s", out)
	}
}
