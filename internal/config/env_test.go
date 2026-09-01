package config

import (
	"strings"
	"testing"
)

// TestEnvVarNesting exercises the OMNI_* / "__" nesting rules from
// internal-docs/08-configuration.md §Environment variables, including the
// distinction between "__" (nesting) and "_" (part of a key's own name,
// e.g. all_traffic / idle_timeout / on_unrepresentable).
func TestEnvVarNesting(t *testing.T) {
	home := testHome(t)
	testCWD(t)

	t.Setenv("OMNI_RECORD__REDACT", "false")
	t.Setenv("OMNI_ALL_TRAFFIC", "true")
	t.Setenv("OMNI_PROXY__IDLE_TIMEOUT", "5m")
	t.Setenv("OMNI_ADAPT__ON_UNREPRESENTABLE", "warn")
	t.Setenv("OMNI_AGENTS__CLAUDE__BINARY", "/opt/claude")

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v (issues: %+v)", err, e.Issues)
	}
	if e.Record.Redact.V != false {
		t.Errorf("record.redact = %v, want false", e.Record.Redact.V)
	}
	if e.Record.Redact.Source != "$OMNI_RECORD__REDACT" {
		t.Errorf("record.redact source = %q", e.Record.Redact.Source)
	}
	if e.AllTraffic.V != true {
		t.Errorf("all_traffic = %v, want true", e.AllTraffic.V)
	}
	if got := e.Proxy.IdleTimeout.V.String(); got != "5m0s" {
		t.Errorf("proxy.idle_timeout = %q, want 5m0s", got)
	}
	if e.Adapt.OnUnrepresentable.V != "warn" {
		t.Errorf("adapt.on_unrepresentable = %q, want warn", e.Adapt.OnUnrepresentable.V)
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
	t.Setenv("OMNI_ALL_TRAFFIC", "not-a-bool")

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	found := false
	for _, is := range e.Issues {
		if is.Path == "all_traffic" && is.Level == LevelError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a LevelError issue for OMNI_ALL_TRAFFIC=not-a-bool, got: %+v", e.Issues)
	}
	// Falls back to the previous value rather than a zero value silently.
	if e.AllTraffic.V != false || e.AllTraffic.Source != builtinSource {
		t.Errorf("all_traffic = %+v, want untouched built-in default after a bad env value", e.AllTraffic)
	}
}

// TestEnvVarModelMapUnsupported checks that model_map (a map-valued field)
// cannot be set via environment variables, and that attempting it produces
// a warning rather than silently doing nothing unexplained.
func TestEnvVarModelMapUnsupported(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	t.Setenv("OMNI_AGENTS__CLAUDE__MODEL_MAP__CLAUDE_OPUS_5", "claude-sonnet-5")

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(e.ModelMap.V) != 0 {
		t.Errorf("model_map = %v, want empty (env cannot set it)", e.ModelMap.V)
	}
	found := false
	for _, is := range e.Issues {
		if is.Level == LevelWarning && strings.Contains(is.Message, "model_map") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a warning issue explaining model_map cannot be set via env, got: %+v", e.Issues)
	}
}

// TestOverrideLayer checks (*Effective).Override, the hook cmd/omni uses
// for layer 7 (CLI flags), including that it rejects unknown keys and
// still enforces Fatal checks (e.g. a credential-shaped override value).
func TestOverrideLayer(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := e.Override(map[string]string{"mode": "route"}, "(cli flag)"); err != nil {
		t.Fatalf("Override: %v", err)
	}
	if e.Mode.V != ModeRoute || e.Mode.Source != "(cli flag)" {
		t.Fatalf("mode = %+v, want route/(cli flag)", e.Mode)
	}

	if err := e.Override(map[string]string{"not_a_real_key": "x"}, "(cli flag)"); err == nil {
		t.Errorf("Override: expected error for unknown key")
	}

	if err := e.Override(map[string]string{"proxy.listen": "0.0.0.0:1"}, "(cli flag)"); err == nil {
		t.Errorf("Override: expected error for non-loopback proxy.listen")
	}
}
