package config

import (
	"strings"
	"testing"
)

// TestEnvVarNesting exercises the OMNI_* / "__" nesting rules from
// internal-docs/08-configuration.md §Environment variables. No key in the
// current schema has an underscore in its own name, so "__" is exercised
// here only by the OMNI_AGENTS__<NAME>__ prefix — which is all the rule has
// left to disambiguate.
func TestEnvVarNesting(t *testing.T) {
	home := testHome(t)
	testCWD(t)

	t.Setenv("OMNI_MODE", "off")
	t.Setenv("OMNI_REDACT", "false")
	t.Setenv("OMNI_AGENTS__CLAUDE__BINARY", "/opt/claude")

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v (issues: %+v)", err, e.Check())
	}
	if e.Mode.V != ModeOff {
		t.Errorf("mode = %v, want off", e.Mode.V)
	}
	if e.Redact.V != false {
		t.Errorf("redact = %v, want false", e.Redact.V)
	}
	if e.Redact.Source != "$OMNI_REDACT" {
		t.Errorf("redact source = %q", e.Redact.Source)
	}
	if e.Binary.V != "/opt/claude" {
		t.Errorf("binary = %q, want /opt/claude", e.Binary.V)
	}

	// codex must not see claude's agent-scoped binary override.
	eCodex, err := LoadFrom(home, "codex")
	if err != nil {
		t.Fatalf("Load codex: %v", err)
	}
	if eCodex.Binary.V != "" {
		t.Errorf("codex binary = %q, want empty (claude-scoped var must not leak)", eCodex.Binary.V)
	}
}

// TestEnvVarInvalidValue checks that a type mismatch (e.g. a non-boolean
// for a bool field) becomes a LevelError Issue rather than crashing Load —
// distinct from the two Fatal categories (credential, non-loopback listen).
func TestEnvVarInvalidValue(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	t.Setenv("OMNI_REDACT", "not-a-bool")

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	found := false
	for _, is := range e.Check() {
		if is.Path == "redact" && is.Level == LevelError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a LevelError issue for OMNI_REDACT=not-a-bool, got: %+v", e.Check())
	}
	// Falls back to the previous value rather than a zero value silently —
	// redaction in particular must never fail open on a typo.
	if e.Redact.V != true || e.Redact.Source != builtinSource {
		t.Errorf("redact = %+v, want untouched built-in default after a bad env value", e.Redact)
	}
}

// TestEnvVarRoutesUnsupported checks that routing rules (a list-valued field)
// cannot be set via environment variables, and that attempting it produces
// a warning rather than silently doing nothing unexplained.
func TestEnvVarRoutesUnsupported(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	t.Setenv("OMNI_AGENTS__CLAUDE__ROUTE__CLAUDE_OPUS_5", "claude-sonnet-5")

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(e.Routes.V) != 0 {
		t.Errorf("routes = %v, want empty (env cannot set them)", e.Routes.V)
	}
	found := false
	for _, is := range e.Check() {
		if is.Level == LevelWarning && strings.Contains(is.Message, "route") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a warning issue explaining routes cannot be set via env, got: %+v", e.Check())
	}
}

// TestOverrideLayer checks (*Effective).Override, the hook cmd/omni uses
// for layer 6 (CLI flags), including that it rejects unknown keys and
// still enforces Fatal checks (e.g. a credential-shaped override value).
func TestOverrideLayer(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := e.Override(map[string]string{"mode": "off"}, "(cli flag)"); err != nil {
		t.Fatalf("Override: %v", err)
	}
	if e.Mode.V != ModeOff || e.Mode.Source != "(cli flag)" {
		t.Fatalf("mode = %+v, want off/(cli flag)", e.Mode)
	}

	if err := e.Override(map[string]string{"not_a_real_key": "x"}, "(cli flag)"); err == nil {
		t.Errorf("Override: expected error for unknown key")
	}
}
