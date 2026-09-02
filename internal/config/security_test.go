package config

import (
	"strings"
	"testing"
)

// TestCredentialRejection checks that a credential-shaped value anywhere in
// config is rejected at Load, per internal-docs/08-configuration.md
// §Security: "No credentials in config. Ever."
func TestCredentialRejection(t *testing.T) {
	cases := []struct {
		name string
		toml string
	}{
		{"anthropic key in upstream", `upstream = "https://sk-ant-api03-abcdefghijklmnop"`},
		{"generic sk- key in binary", `binary = "sk-abcdefghijklmnopqrstuvwx"`},
		{"bearer token in env", "[env]\nFOO = \"Bearer abcdefghijklmnopqrstuvwx\""},
		{"key as route model", "[[route]]\nmatch = \"claude-opus-5\"\nmodel = \"sk-ant-api03-abcdefghijklmnop\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := testHome(t)
			testCWD(t)
			writeAgentSection(t, home, "claude", tc.toml)

			e, err := LoadFrom(home, "claude")
			if err == nil {
				t.Fatalf("Load: expected error for credential-shaped value, got nil (issues: %+v)", e.Check())
			}
			if e == nil || !e.HasFatal() {
				t.Fatalf("expected a Fatal issue recorded even though Load errored; got %+v", e)
			}
			foundCred := false
			for _, is := range e.Check() {
				if is.Level == LevelFatal && strings.Contains(is.Message, "credential") {
					foundCred = true
				}
			}
			if !foundCred {
				t.Errorf("expected a Fatal credential issue, got: %+v", e.Check())
			}
		})
	}

	t.Run("ordinary values are not flagged", func(t *testing.T) {
		home := testHome(t)
		testCWD(t)
		writeAgentSection(t, home, "claude", `
binary = "/Users/me/.local/bin/claude"
upstream = "https://api.anthropic.com"

[[route]]
match = "claude-opus-5"
model = "claude-sonnet-5"
`)
		e, err := LoadFrom(home, "claude")
		if err != nil {
			t.Fatalf("Load: unexpected error: %v (issues: %+v)", err, e.Check())
		}
		if e.HasFatal() {
			t.Errorf("unexpected Fatal issue on ordinary config: %+v", e.Check())
		}
	})
}

// No config key names a bind address: omni picks loopback on an ephemeral
// port, and internal/proxy enforces that. See TestNonLoopbackBindRejected
// and TestLoopbackBindAccepted there.

// TestRoutingCapability checks that routing rules on codex (openai style,
// cannot rewrite) is flagged as an error naming the limitation, per
// internal-docs/08-configuration.md's `config check` semantics.
func TestRoutingCapability(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeAgentSection(t, home, "codex", `
[[route]]
match = "gpt-5"
model = "gpt-5-mini"
`)
	e, err := LoadFrom(home, "codex")
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	found := false
	for _, is := range e.Check() {
		if is.Path == "route" && is.Level == LevelError {
			found = true
			if !strings.Contains(is.Message, "codex") {
				t.Errorf("message should name the agent: %q", is.Message)
			}
		}
	}
	if !found {
		t.Errorf("expected a LevelError route issue for codex, got: %+v", e.Check())
	}
	if e.HasFatal() {
		t.Errorf("routing capability mismatch should not be Fatal (does not block Load), got: %+v", e.Check())
	}
}
