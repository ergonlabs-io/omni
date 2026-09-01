package profile

import "testing"

func TestBuiltinsRegistered(t *testing.T) {
	for _, name := range []string{"claude", "codex"} {
		if Lookup(name) == nil {
			t.Errorf("builtin profile %q not registered", name)
		}
	}
}

func TestAliasesResolve(t *testing.T) {
	if p := Lookup("cc"); p == nil || p.Name != "claude" {
		t.Errorf("alias cc did not resolve to claude, got %v", p)
	}
}

func TestNoProfileUsesReservedName(t *testing.T) {
	for _, p := range All() {
		if IsReserved(p.Name) {
			t.Errorf("profile %q collides with reserved subcommand namespace", p.Name)
		}
		for _, a := range p.Aliases {
			if IsReserved(a) {
				t.Errorf("alias %q collides with reserved subcommand namespace", a)
			}
		}
	}
}

func TestEnvSteersBaseURL(t *testing.T) {
	p := Lookup("claude")
	env := p.Env("http://127.0.0.1:1234", "")
	want := "ANTHROPIC_BASE_URL=http://127.0.0.1:1234"
	if len(env) != 1 || env[0] != want {
		t.Errorf("Env() = %v, want [%s]", env, want)
	}
}

func TestEnvIncludesTrustOnlyWithCA(t *testing.T) {
	p := Lookup("claude")
	if got := p.Env("http://x", ""); len(got) != 1 {
		t.Errorf("no CA path should yield only base URL, got %v", got)
	}
	got := p.Env("http://x", "/tmp/ca.pem")
	if len(got) != 2 || got[1] != "NODE_EXTRA_CA_CERTS=/tmp/ca.pem" {
		t.Errorf("Env() with CA = %v", got)
	}
}

// Codex has no confirmed CA trust mechanism, so Tier 2 must report unsupported
// rather than silently producing unintercepted traffic. See docs/03.
func TestCodexTier2Unsupported(t *testing.T) {
	if Lookup("codex").SupportsTier2() {
		t.Error("codex claims Tier 2 support; docs/03 marks its TLS backend unconfirmed")
	}
	if !Lookup("claude").SupportsTier2() {
		t.Error("claude should support Tier 2 via NODE_EXTRA_CA_CERTS")
	}
}

func TestOnlyAnthropicIsRewritable(t *testing.T) {
	if !StyleAnthropic.CanRewrite() {
		t.Error("anthropic style must be rewritable")
	}
	if StyleOpenAI.CanRewrite() || StylePassthrough.CanRewrite() {
		t.Error("v1 rewrites Anthropic only; see docs/04 'Scope for v1'")
	}
}

func TestSuggestFindsNearMiss(t *testing.T) {
	got := Suggest("cluade")
	if len(got) == 0 || got[0] != "claude" {
		t.Errorf("Suggest(cluade) = %v, want [claude]", got)
	}
}
