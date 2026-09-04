package profile

// Built-in agent profiles.
//
// Several fields here are marked [ASSUMED] in internal-docs/03-agent-profiles.md
// and must be confirmed against real captured traffic in Phase 0. Where a value
// is unconfirmed it is left conservative (Tier 2 unsupported, passthrough
// style) so omni fails loudly rather than silently mis-handling traffic.

func init() {
	register(&Profile{
		Name:       "claude",
		Aliases:    []string{"claude-code", "cc"},
		Binary:     "claude",
		Desc:       "Claude Code",
		BaseURLEnv: "ANTHROPIC_BASE_URL",
		Upstream:   "https://api.anthropic.com",
		APIStyle:   StyleAnthropic,
		// Node reads NODE_EXTRA_CA_CERTS. Reliable.
		TrustEnv: []string{"NODE_EXTRA_CA_CERTS"},
	})

	register(&Profile{
		Name:   "codex",
		Binary: "codex",
		Desc:   "Codex",
		// OPENAI_BASE_URL is observed, not assumed — and it is not enough.
		// Codex reads it, but under ChatGPT auth it redirects only the
		// ancillary endpoints (plugins, analytics, MCP, settings). The model
		// call follows the base_url of the [model_providers.*] entry in
		// Codex's own config, and nothing else moves it. `chatgpt_base_url`
		// does not move it either.
		//
		// It is kept because it is correct for API-key auth and costs
		// nothing, but it must not be read as "codex is intercepted". Making
		// that true needs the user to point a model_provider at omni against
		// a pinned listen_port. Verified against codex 0.112.0: a POST to
		// /responses, plain HTTP, 53KB body, top-level "model".
		BaseURLEnv: "OPENAI_BASE_URL",
		Upstream:   "https://api.openai.com",
		APIStyle:   StyleOpenAI,
		// Confirmed Rust, and the binary carries rustls and its own webpki
		// trust anchors, which is the case that ignores every CA env var.
		// Tier 2 stays unsupported: SupportsTier2() == false makes
		// --all-traffic error clearly instead of silently not intercepting.
		TrustEnv: nil,
	})
}
