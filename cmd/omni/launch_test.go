package main

import (
	"strings"
	"testing"

	"github.com/ergonlabs-io/omni/internal/profile"
)

// resolveEnv returns the value exec would use for key: the last occurrence
// wins, matching os/exec's documented handling of duplicate keys.
func resolveEnv(env []string, key string) (string, bool) {
	val, ok := "", false
	for _, kv := range env {
		if k, v, found := strings.Cut(kv, "="); found && k == key {
			val, ok = v, true
		}
	}
	return val, ok
}

// The base-URL variable is the whole of Tier 1 interception. A user [env]
// entry that shadowed it would not fail loudly — it would send the agent
// straight to the upstream while omni still reported a live session and
// wrote an empty recording.
func TestSteeringEnvBeatsUserEnv(t *testing.T) {
	p := &profile.Profile{
		Name:       "claude",
		BaseURLEnv: "ANTHROPIC_BASE_URL",
	}
	const proxyURL = "http://127.0.0.1:54312"

	base := []string{
		"PATH=/usr/bin:/bin",
		"ANTHROPIC_BASE_URL=https://inherited.example",
	}
	user := map[string]string{
		"ANTHROPIC_BASE_URL": "https://user-override.example",
		"ANTHROPIC_LOG":      "debug",
	}

	env := childEnv(base, user, p.Env(proxyURL, ""))

	if got, ok := resolveEnv(env, "ANTHROPIC_BASE_URL"); !ok || got != proxyURL {
		t.Errorf("ANTHROPIC_BASE_URL = %q (present: %v), want %q — a user [env] entry must never redirect the agent",
			got, ok, proxyURL)
	}
	if got, _ := resolveEnv(env, "ANTHROPIC_LOG"); got != "debug" {
		t.Errorf("ANTHROPIC_LOG = %q, want debug — non-conflicting user [env] must still be applied", got)
	}
	if got, _ := resolveEnv(env, "PATH"); got != "/usr/bin:/bin" {
		t.Errorf("PATH = %q, want the inherited value", got)
	}
}

// Tier 2 injects trust variables alongside the base URL; they carry the same
// guarantee. Exercised here even though --all-traffic is not wired in yet, so
// the invariant is already covered when it lands.
func TestSteeringEnvBeatsUserEnvForTrustVars(t *testing.T) {
	p := &profile.Profile{
		Name:       "claude",
		BaseURLEnv: "ANTHROPIC_BASE_URL",
		TrustEnv:   []string{"NODE_EXTRA_CA_CERTS"},
	}
	const caPath = "/home/me/.omni/ca/omni-ca.pem"

	env := childEnv(
		[]string{"PATH=/usr/bin"},
		map[string]string{"NODE_EXTRA_CA_CERTS": "/tmp/attacker.pem"},
		p.Env("http://127.0.0.1:54312", caPath),
	)

	if got, _ := resolveEnv(env, "NODE_EXTRA_CA_CERTS"); got != caPath {
		t.Errorf("NODE_EXTRA_CA_CERTS = %q, want %q", got, caPath)
	}
}

// childEnv must not mutate or alias the slice it is handed: launch passes
// os.Environ(), and a later append that reused its backing array would
// corrupt the caller's view of the environment.
func TestChildEnvDoesNotAliasBase(t *testing.T) {
	base := make([]string, 2, 8) // spare capacity: append would write in place
	base[0], base[1] = "PATH=/usr/bin", "HOME=/home/me"

	env := childEnv(base, map[string]string{"FOO": "bar"}, []string{"ANTHROPIC_BASE_URL=http://127.0.0.1:1"})

	if len(base) != 2 || base[0] != "PATH=/usr/bin" || base[1] != "HOME=/home/me" {
		t.Errorf("childEnv mutated its base slice: %v", base)
	}
	if len(env) != 4 {
		t.Errorf("len(env) = %d, want 4: %v", len(env), env)
	}
}
