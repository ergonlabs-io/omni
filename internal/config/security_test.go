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
// TestRoutingCapability pins what a codex rule may and may not do.
//
// Rewriting for the OpenAI style landed once Codex's request was actually
// observed: a POST to /responses whose top-level "model" is the single
// field routing splices. So a same-style rule now resolves instead of being
// refused.
//
// The security property this file cares about is unchanged and asserted
// below: a rule may not cross wire formats. Routing a codex rule at an
// Anthropic backend would require translation, which omni does not do, and
// is still refused.
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
	for _, is := range e.Check() {
		if is.Path == "route" && is.Level == LevelError {
			t.Errorf("same-style codex rule should now resolve, got: %+v", is)
		}
	}

	// Crossing styles remains refused: that is translation, not routing.
	home2 := testHome(t)
	writeTestFile(t, GlobalConfigPath(home2), `
[backends.anth]
base_url    = "https://api.anthropic.com"
api_key_env = "ANTHROPIC_API_KEY"
api_style   = "anthropic"

[[agents.codex.route]]
match   = "gpt-5"
backend = "anth"
`)
	e2, err := LoadFrom(home2, "codex")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, issues := e2.Resolve("openai")
	found := false
	for _, is := range issues {
		if is.Level == LevelError && strings.Contains(is.Message, "does not translate") {
			found = true
		}
	}
	if !found {
		t.Errorf("cross-style codex rule must still be refused, got: %+v", issues)
	}
}
