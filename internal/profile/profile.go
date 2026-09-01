// Package profile is the agent registry: how to launch each supported coding
// agent, and how to steer its HTTP traffic into omni's proxy.
//
// Adding an agent should be a struct literal. See internal-docs/03-agent-profiles.md.
package profile

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// APIStyle names the wire schema an agent speaks. It determines whether omni
// can decode (and therefore rewrite) request bodies, or must treat them as
// opaque bytes.
type APIStyle string

const (
	// StyleAnthropic is the Anthropic Messages API. The only style omni can
	// rewrite in v1.
	StyleAnthropic APIStyle = "anthropic"
	// StyleOpenAI is the OpenAI Chat Completions API. Recorded, not rewritten.
	StyleOpenAI APIStyle = "openai"
	// StylePassthrough means the wire format is unmodeled: record bytes, never
	// decode. The correct default for any agent we have not studied.
	StylePassthrough APIStyle = "passthrough"
)

// CanRewrite reports whether omni can decode and mutate bodies in this style.
func (s APIStyle) CanRewrite() bool { return s == StyleAnthropic }

// Profile describes one supported agent.
type Profile struct {
	// Name is the canonical agent name, as typed: `omni <name>`.
	Name string
	// Aliases are alternative names resolving to this profile.
	Aliases []string
	// Binary is the executable looked up on PATH.
	Binary string
	// Desc is a short human label for --help.
	Desc string

	// BaseURLEnv is the environment variable that redirects this agent's API
	// calls at omni's proxy (Tier 1 interception). Empty means the agent cannot
	// be steered by environment alone and requires Shim.
	BaseURLEnv string
	// Upstream is the real endpoint omni forwards to.
	Upstream string
	// APIStyle is the wire schema for bodies on Upstream.
	APIStyle APIStyle

	// TrustEnv lists environment variables that make this agent's runtime trust
	// omni's CA (Tier 2 full MITM). Empty means Tier 2 is unsupported for this
	// agent — omni must say so rather than silently leaving traffic
	// unintercepted.
	TrustEnv []string

	// Shim optionally writes a temporary config file for agents that cannot be
	// steered by environment alone, returning extra env and a cleanup func.
	// Nil for env-steerable agents.
	Shim func(LaunchContext) (env []string, cleanup func(), err error)
}

// LaunchContext is what a Shim needs to write its config.
type LaunchContext struct {
	// ProxyURL is omni's listening address, e.g. "http://127.0.0.1:54312".
	ProxyURL string
	// CAPath is the PEM path for Tier 2, or "" when Tier 2 is not active.
	CAPath string
	// OmniHome is the resolved $OMNI_HOME (or ~/.omni).
	OmniHome string
}

// SupportsTier2 reports whether full-MITM interception is possible for this
// agent. When false, --all-traffic must fail loudly rather than degrade.
func (p *Profile) SupportsTier2() bool { return len(p.TrustEnv) > 0 }

// Env returns the environment additions that steer this agent into omni.
// caPath is empty when Tier 2 is not active.
//
// These values always win over user-supplied [env] config: omni cannot do its
// job if the agent is pointed somewhere else.
func (p *Profile) Env(proxyURL, caPath string) []string {
	var env []string
	if p.BaseURLEnv != "" {
		env = append(env, p.BaseURLEnv+"="+proxyURL)
	}
	if caPath != "" {
		for _, k := range p.TrustEnv {
			env = append(env, k+"="+caPath)
		}
	}
	return env
}

// Resolve looks up the agent binary on PATH.
func (p *Profile) Resolve() (string, error) {
	path, err := exec.LookPath(p.Binary)
	if err != nil {
		return "", fmt.Errorf("agent %q: binary %q not found on PATH", p.Name, p.Binary)
	}
	return path, nil
}

// Reserved names can never be agent names; they always resolve as subcommands.
// See internal-docs/09-cli-design.md §2.
var Reserved = []string{
	"init", "config", "ca", "version", "help", "completions", "sessions", "run",
}

// IsReserved reports whether name collides with the subcommand namespace.
func IsReserved(name string) bool {
	for _, r := range Reserved {
		if name == r {
			return true
		}
	}
	return false
}

var registry = map[string]*Profile{}

func register(p *Profile) {
	registry[p.Name] = p
	for _, a := range p.Aliases {
		registry[a] = p
	}
}

// Lookup returns the profile for name, or nil.
func Lookup(name string) *Profile { return registry[name] }

// All returns every registered profile, deduplicated and sorted by name.
func All() []*Profile {
	seen := map[string]bool{}
	var out []*Profile
	for _, p := range registry {
		if !seen[p.Name] {
			seen[p.Name] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names returns all canonical profile names, sorted.
func Names() []string {
	var out []string
	for _, p := range All() {
		out = append(out, p.Name)
	}
	return out
}

// Suggest returns names within edit distance 2 of input, for "did you mean".
func Suggest(input string) []string {
	var out []string
	for _, n := range Names() {
		if levenshtein(strings.ToLower(input), n) <= 2 {
			out = append(out, n)
		}
	}
	return out
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
