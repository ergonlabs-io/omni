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
			writeTestFile(t, AgentConfigPath(home, "claude"), tc.toml)

			e, err := LoadFrom(home, "claude")
			if err == nil {
				t.Fatalf("Load: expected error for credential-shaped value, got nil (issues: %+v)", e.Check())
			}
			if e == nil || !e.HasFatal() {
				t.Fatalf("expected a Fatal issue recorded even though Load errored; got %+v", e)
			}
			foundCred := false
			for _, is := range e.Check() {
				if is.Fatal && strings.Contains(is.Message, "credential") {
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
		writeTestFile(t, AgentConfigPath(home, "claude"), `
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

// TestProxyListenLoopbackOnly checks that a non-loopback proxy.listen is
// rejected at Load, not honored, per internal-docs/08-configuration.md
// §Security.
func TestProxyListenLoopbackOnly(t *testing.T) {
	rejected := []string{
		"0.0.0.0:8080",
		"8.8.8.8:8080",
		"[::]:8080",
		"example.com:8080",
	}
	for _, addr := range rejected {
		t.Run(addr, func(t *testing.T) {
			home := testHome(t)
			testCWD(t)
			writeTestFile(t, GlobalConfigPath(home), `
[defaults.proxy]
listen = "`+addr+`"
`)
			e, err := LoadFrom(home, "claude")
			if err == nil {
				t.Fatalf("Load: expected error for non-loopback listen %q, got nil", addr)
			}
			if e == nil || !e.HasFatal() {
				t.Fatalf("expected a Fatal issue for proxy.listen=%q", addr)
			}
		})
	}

	accepted := []string{
		"127.0.0.1:0",
		"127.0.0.1:54321",
		"localhost:0",
		"[::1]:0",
	}
	for _, addr := range accepted {
		t.Run(addr, func(t *testing.T) {
			home := testHome(t)
			testCWD(t)
			writeTestFile(t, GlobalConfigPath(home), `
[defaults.proxy]
listen = "`+addr+`"
`)
			e, err := LoadFrom(home, "claude")
			if err != nil {
				t.Fatalf("Load: unexpected error for loopback listen %q: %v (issues: %+v)", addr, err, e.Check())
			}
			if e.Proxy.Listen.V != addr {
				t.Errorf("proxy.listen = %q, want %q", e.Proxy.Listen.V, addr)
			}
		})
	}
}

// TestRoutingCapability checks that routing rules on codex (openai style,
// cannot rewrite) is flagged as an error naming the limitation, per
// internal-docs/08-configuration.md's `config check` semantics.
func TestRoutingCapability(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeTestFile(t, AgentConfigPath(home, "codex"), `
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

	if err := e.RoutingError(false); err == nil {
		t.Errorf("RoutingError(false) = nil, want an error naming the limitation")
	} else if !strings.Contains(err.Error(), "codex") {
		t.Errorf("RoutingError message = %q, want it to name codex", err.Error())
	}
	if err := e.RoutingError(true); err != nil {
		t.Errorf("RoutingError(true) = %v, want nil", err)
	}
}
