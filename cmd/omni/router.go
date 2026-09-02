package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/ergonlabs-io/omni/internal/config"
	"github.com/ergonlabs-io/omni/internal/profile"
	"github.com/ergonlabs-io/omni/internal/proxy"
)

// resolveRouter turns the effective config's routing rules and backends
// into a proxy.Router, reading each targeted backend's credential from the
// environment.
//
// It returns (nil, nil) when there is nothing to route — either mode is not
// "route", or the rule list is empty. Routing is deliberately gated on mode
// rather than on the mere presence of rules: a [[route]] left in a config
// file must not start rerouting traffic (and spending money elsewhere) just
// because it is syntactically present.
//
// A missing credential is an error here, not a warning. config.Check only
// warns, because a backend may be declared and unused; by this point a rule
// actually targets it, and launching an agent whose first matching request
// will 401 is worse than refusing to launch.
func resolveRouter(eff *config.Effective, p *profile.Profile) (*proxy.Router, error) {
	// Routing is on whenever rules exist, unless the whole proxy is off.
	if eff.Mode.V == config.ModeOff || len(eff.Routes.V) == 0 {
		return nil, nil
	}

	resolved, issues := eff.Resolve(string(p.APIStyle))
	for _, is := range issues {
		if is.Level == config.LevelError {
			return nil, fmt.Errorf("%s: %s (%s)", is.Path, is.Message, is.Source)
		}
	}

	upstream := p.Upstream
	if eff.Upstream.V != "" {
		upstream = eff.Upstream.V
	}

	// One *proxy.Backend per distinct backend, so every rule targeting the
	// same backend shares a pointer and the credential is read once.
	backends := map[string]*proxy.Backend{}
	rules := make([]proxy.Rule, 0, len(resolved))

	for _, r := range resolved {
		pr := proxy.Rule{Match: r.Match, Model: r.Model}
		if r.Backend != nil {
			b, ok := backends[r.Backend.Name]
			if !ok {
				var err error
				b, err = buildBackend(*r.Backend, upstream)
				if err != nil {
					return nil, err
				}
				backends[r.Backend.Name] = b
			}
			pr.Backend = b
		}
		rules = append(rules, pr)
	}
	return proxy.NewRouter(rules), nil
}

// buildBackend resolves one config backend into the form the proxy wants,
// including the credential decision. agentUpstream is what the agent would
// have talked to without any routing.
func buildBackend(b config.Backend, agentUpstream string) (*proxy.Backend, error) {
	u, err := url.Parse(b.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("backends.%s.base_url %q: %w", b.Name, b.BaseURL, err)
	}
	out := &proxy.Backend{Name: b.Name, URL: u, Headers: b.Headers}

	switch b.Policy(agentUpstream) {
	case config.AuthSubstitute:
		key := os.Getenv(b.APIKeyEnv)
		if key == "" {
			return nil, fmt.Errorf(
				"backend %q needs $%s, which is not set — export it, or remove the rules targeting %q",
				b.Name, b.APIKeyEnv, b.Name,
			)
		}
		out.APIKey = key
	case config.AuthPreserve:
		// Same host the agent would have reached anyway: there is no
		// trust boundary here, so its own credentials go through untouched.
		out.PreserveAuth = true
	case config.AuthNone:
		// A local endpoint wanting no auth. The agent's credential is still
		// stripped; nothing replaces it.
	}
	return out, nil
}

// applyModelMapFlags turns each `--model-map <from>=<to>` into a routing
// rule and puts them ahead of the configured ones.
//
// They go first because CLI flags are the highest precedence layer
// (internal-docs/08-configuration.md §Precedence) and routing is
// first-match-wins: prepending is what "highest precedence" means for an
// ordered list. `from` is a glob like any other rule's match, so
// `--model-map 'claude-opus-*=claude-sonnet-5'` works as it reads.
func applyModelMapFlags(eff *config.Effective, flags []string) error {
	if len(flags) == 0 {
		return nil
	}
	rules := make([]config.Rule, 0, len(flags)+len(eff.Routes.V))
	for _, f := range flags {
		from, to, ok := strings.Cut(f, "=")
		if !ok || from == "" || to == "" {
			return fmt.Errorf("--model-map %q: want <from>=<to>", f)
		}
		rules = append(rules, config.Rule{Match: from, Model: to, Source: "command line"})
	}
	eff.Routes = config.Value[[]config.Rule]{
		V:      append(rules, eff.Routes.V...),
		Source: "command line",
	}
	return nil
}
