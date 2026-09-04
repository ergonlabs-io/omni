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

// TestModeledStylesAreRewritable pins which wire formats omni will decode.
//
// Anthropic and OpenAI both carry the model as a top-level "model" string,
// which is the only field routing touches, so both are rewritable. This
// does NOT open cross-provider translation: a rule may only target a
// backend of the agent's own style, which Effective.Resolve enforces
// separately and TestAPIStyleMismatchRejected pins.
//
// Passthrough stays false by definition — an unmodeled body is the one omni
// promised never to decode.
func TestModeledStylesAreRewritable(t *testing.T) {
	if !StyleAnthropic.CanRewrite() {
		t.Error("anthropic style must be rewritable")
	}
	if !StyleOpenAI.CanRewrite() {
		t.Error("openai style must be rewritable: Codex posts a top-level model field")
	}
	if StylePassthrough.CanRewrite() {
		t.Error("passthrough must never be decoded")
	}
}

func TestSuggestFindsNearMiss(t *testing.T) {
	got := Suggest("cluade")
	if len(got) == 0 || got[0] != "claude" {
		t.Errorf("Suggest(cluade) = %v, want [claude]", got)
	}
}
