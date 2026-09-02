package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/ergonlabs-io/omni/internal/profile"
)

// credentialPatterns match the key shapes internal-docs/08-configuration.md
// §Security calls out by name: an Anthropic API key ("sk-ant-..."), a
// generic OpenAI-style secret key ("sk-..."), and a bearer token. Config
// values are never credentials — they come from the environment or the
// agent's own auth — so any match is rejected wherever it appears.
var credentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{6,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._-]{16,}`),
}

// looksLikeCredential reports whether s matches a known credential shape.
func looksLikeCredential(s string) bool {
	for _, re := range credentialPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// runChecks performs the semantic validation that cannot be done purely
// while decoding one layer: it needs the fully merged value (credential
// scan, loopback check) or an outside fact (the agent's APIStyle, for the
// routing capability check). Called by both Load and Override so a Fatal
// Issue is caught the moment it becomes true, not only when a caller
// remembers to invoke Check.
func runChecks(e *Effective) {
	// Rebuilt, not appended to: Override calls runChecks a second time on
	// the same *Effective, and every derived check would otherwise report
	// its finding once per call.
	e.checkIssues = nil
	scanCredentials(e)
	checkRoutingCapability(e)
	checkRoutes(e)
	checkBackendCredentials(e)
	checkBackendKeys(e)
}

// scanCredentials rejects any string value in the merged config that looks
// like a credential, wherever it appears. This is Fatal — the one Fatal
// category — because Load and Override both refuse to hand back a
// configuration in this state, per internal-docs/08-configuration.md's
// "No credentials in config. Ever."
func scanCredentials(e *Effective) {
	check := func(path, source, val string) {
		if val != "" && looksLikeCredential(val) {
			e.checkIssues = append(e.checkIssues, Issue{
				Path: path,
				Message: "value looks like a credential (API key or bearer token) — " +
					"never put credentials in config; they come from the environment " +
					"or the agent's own auth",
				Source: source,
				Level:  LevelFatal,
			})
		}
	}
	check("binary", e.Binary.Source, e.Binary.V)
	check("upstream", e.Upstream.Source, e.Upstream.V)
	for i, r := range e.Routes.V {
		path := fmt.Sprintf("route[%d]", i)
		check(path+".match", r.Source, r.Match)
		check(path+".model", r.Source, r.Model)
	}
	for k, v := range e.Env.V {
		check("env."+k, e.Env.Source, k)
		check("env."+k, e.Env.Source, v)
	}
	for name, b := range e.Backends.V {
		check("backends."+name+".base_url", b.Source, b.BaseURL)
		// api_key_env holds a variable *name*, so a credential-shaped value
		// here means someone pasted the key itself into the wrong field —
		// exactly the mistake worth catching loudly.
		check("backends."+name+".api_key_env", b.Source, b.APIKeyEnv)
		for hk, hv := range b.Headers {
			check("backends."+name+".headers."+hk, b.Source, hv)
		}
	}
}

// checkRoutes resolves the rule list against the declared backends and
// records whatever will not resolve: an undeclared backend, a backend whose
// wire format the agent does not speak, or a rule an earlier rule shadows.
// Routing is decided before the child launches, so these surface now rather
// than as a failed request twenty minutes into a session.
func checkRoutes(e *Effective) {
	if len(e.Routes.V) == 0 {
		return
	}
	style := ""
	if p := profile.Lookup(e.Agent); p != nil {
		style = string(p.APIStyle)
	}
	_, issues := e.Resolve(style)
	e.checkIssues = append(e.checkIssues, issues...)
}

// checkBackendCredentials rejects a remote backend that names no
// api_key_env and is not the agent's own upstream. Such a backend would be
// sent requests with the agent's credential stripped and nothing put in its
// place — a guaranteed 401, and a config the user plainly did not mean.
// A loopback backend is exempt: a local inference server wanting no auth is
// the normal case.
func checkBackendCredentials(e *Effective) {
	upstream := ""
	if p := profile.Lookup(e.Agent); p != nil {
		upstream = p.Upstream
	}
	if e.Upstream.V != "" {
		upstream = e.Upstream.V
	}
	for _, name := range sortedBackendNames(e.Backends.V) {
		b := e.Backends.V[name]
		if b.APIKeyEnv != "" || b.Policy(upstream) != AuthNone {
			continue
		}
		if u, err := url.Parse(b.BaseURL); err == nil && isLoopbackAddr(u.Host) {
			continue
		}
		e.checkIssues = append(e.checkIssues, Issue{
			Path: "backends." + name + ".api_key_env",
			Message: fmt.Sprintf(
				"backend %q is remote and names no api_key_env — omni strips the agent's "+
					"credential before forwarding, so requests would arrive unauthenticated",
				name,
			),
			Source: b.Source,
			Level:  LevelError,
		})
	}
}

// checkBackendKeys warns about a declared backend whose api_key_env is not
// set in the environment. A warning, not an error: a backend can be
// declared and unused, and only a route that actually targets it needs the
// credential. cmd/omni promotes this to a hard failure when such a route
// exists — see resolveRouter.
func checkBackendKeys(e *Effective) {
	for _, name := range sortedBackendNames(e.Backends.V) {
		b := e.Backends.V[name]
		if b.APIKeyEnv == "" || os.Getenv(b.APIKeyEnv) != "" {
			continue
		}
		e.checkIssues = append(e.checkIssues, Issue{
			Path:    "backends." + name + ".api_key_env",
			Message: fmt.Sprintf("$%s is not set in the environment; routes to backend %q will fail", b.APIKeyEnv, name),
			Source:  b.Source,
			Level:   LevelWarning,
		})
	}
}

// isLoopbackAddr reports whether addr (a "host:port", bare host, or bare IP)
// names loopback. Anything it cannot positively identify as loopback is
// treated as not loopback — the safe default for a security check.
func isLoopbackAddr(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// checkRoutingCapability errors (LevelError, not Fatal — this does not
// stop Load, matching the doc's placement of this rule under `config
// check`'s semantic validation rather than under §Security's "reject at
// load" items) when routing rules are set for an agent whose wire style
// cannot be rewritten. See profile.APIStyle.CanRewrite.
func checkRoutingCapability(e *Effective) {
	if len(e.Routes.V) == 0 {
		return
	}
	p := profile.Lookup(e.Agent)
	if p == nil || p.APIStyle.CanRewrite() {
		return
	}
	e.checkIssues = append(e.checkIssues, Issue{
		Path: "route",
		Message: fmt.Sprintf(
			"cannot apply routing rules for agent %q: model rewriting is not supported for its API style (%s) — sessions are recorded but not rewritten",
			e.Agent, p.APIStyle,
		),
		Source: e.Routes.Source,
		Level:  LevelError,
	})
}

// Check returns every validation problem found for this configuration:
// unknown keys (with near-miss suggestions), disallowed project-config
// keys, unparsable durations and enums — all collected while Load merged
// each layer — plus the checks above. This is what `omni config check`
// prints; a nonzero exit corresponds to any Issue at LevelError.
func (e *Effective) Check() []Issue {
	return e.allIssues()
}
