package config

import (
	"fmt"
	"net"
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
// model_map capability check). Called by both Load and Override so a Fatal
// Issue is caught the moment it becomes true, not only when a caller
// remembers to invoke Check.
func runChecks(e *Effective) {
	scanCredentials(e)
	checkLoopback(e)
	checkModelMapCapability(e)
}

// scanCredentials rejects any string value in the merged config that looks
// like a credential, wherever it appears. This is Fatal: Load and Override
// both refuse to hand back a configuration in this state, per
// internal-docs/08-configuration.md's "No credentials in config. Ever."
func scanCredentials(e *Effective) {
	check := func(path, source, val string) {
		if val != "" && looksLikeCredential(val) {
			e.Issues = append(e.Issues, Issue{
				Path: path,
				Message: "value looks like a credential (API key or bearer token) — " +
					"never put credentials in config; they come from the environment " +
					"or the agent's own auth",
				Source: source,
				Level:  LevelError,
				Fatal:  true,
			})
		}
	}
	check("binary", e.Binary.Source, e.Binary.V)
	check("upstream", e.Upstream.Source, e.Upstream.V)
	check("proxy.listen", e.Proxy.Listen.Source, e.Proxy.Listen.V)
	for k, v := range e.ModelMap.V {
		check("model_map."+k, e.ModelMap.Source, k)
		check("model_map."+k, e.ModelMap.Source, v)
	}
	for k, v := range e.Env.V {
		check("env."+k, e.Env.Source, k)
		check("env."+k, e.Env.Source, v)
	}
}

// checkLoopback rejects a proxy.listen that does not resolve to loopback.
// Fatal, per internal-docs/08-configuration.md: "A config that binds
// elsewhere is rejected at load, not honored."
func checkLoopback(e *Effective) {
	addr := e.Proxy.Listen.V
	if isLoopbackAddr(addr) {
		return
	}
	e.Issues = append(e.Issues, Issue{
		Path: "proxy.listen",
		Message: fmt.Sprintf(
			"proxy.listen %q is not loopback — must bind 127.0.0.1, ::1, or localhost; "+
				"a proxy holding live API credentials must not be reachable off-host",
			addr,
		),
		Source: e.Proxy.Listen.Source,
		Level:  LevelError,
		Fatal:  true,
	})
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

// checkModelMapCapability errors (LevelError, not Fatal — this does not
// stop Load, matching the doc's placement of this rule under `config
// check`'s semantic validation rather than under §Security's "reject at
// load" items) when model_map is set for an agent whose wire style cannot
// be rewritten. See profile.APIStyle.CanRewrite.
func checkModelMapCapability(e *Effective) {
	if len(e.ModelMap.V) == 0 {
		return
	}
	p := profile.Lookup(e.Agent)
	if p == nil || p.APIStyle.CanRewrite() {
		return
	}
	e.Issues = append(e.Issues, Issue{
		Path: "model_map",
		Message: fmt.Sprintf(
			"cannot apply model_map for agent %q: model rewriting is not supported for its API style (%s) — sessions are recorded but not rewritten",
			e.Agent, p.APIStyle,
		),
		Source: e.ModelMap.Source,
		Level:  LevelError,
	})
}

// Check returns every validation problem found for this configuration:
// unknown keys (with near-miss suggestions), disallowed project-config
// keys, unparsable durations and enums — all collected while Load merged
// each layer — plus the checks above. This is what `omni config check`
// prints; a nonzero exit corresponds to any Issue at LevelError.
func (e *Effective) Check() []Issue {
	return e.Issues
}
