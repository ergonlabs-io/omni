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
		Name:       "codex",
		Binary:     "codex",
		Desc:       "Codex",
		BaseURLEnv: "OPENAI_BASE_URL", // [ASSUMED] — Phase 0 must confirm
		Upstream:   "https://api.openai.com",
		APIStyle:   StyleOpenAI,
		// [ASSUMED] Codex is Rust. If it uses rustls with bundled webpki-roots
		// it ignores every CA env var and Tier 2 is impossible. Left empty
		// until Phase 0 determines this: SupportsTier2() == false means
		// --all-traffic errors clearly instead of silently not intercepting.
		TrustEnv: nil,
	})
}
