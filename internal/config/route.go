package config

import (
	"fmt"

	"github.com/ergonlabs-io/omni/internal/glob"
)

// Rule is one routing rule: a glob against the model the agent asked for,
// and where that request should go instead.
//
// Rules are an ordered list, first match wins, in file order. That ordering
// is the whole reason this is a list of tables rather than a map: glob
// patterns overlap, and a TOML table has no defined iteration order, so
// overlapping patterns in a map would resolve unpredictably from run to run
// (internal-docs/08-configuration.md §Routing and backends).
type Rule struct {
	// Match is a glob against the model the agent asked for, and nothing
	// else. Matching on the requested model alone is what keeps routing
	// deterministic, and therefore cache-safe
	// (internal-docs/05-constraints.md §1).
	Match string
	// Backend names a declared backend, or is empty to keep the agent's own
	// upstream (a same-endpoint rename).
	Backend string
	// Model is what to ask for instead. Empty falls back to the backend's
	// own model, and then to leaving the requested model unchanged.
	Model string
	// Source is the file:line of this rule.
	Source string
}

// rawRoute mirrors one [[route]] table.
type rawRoute struct {
	Match   *string `toml:"match"`
	Backend *string `toml:"backend"`
	Model   *string `toml:"model"`
}

// knownRoutePaths are the recognized keys inside a [[route]] table.
var knownRoutePaths = map[string]bool{
	"match":   true,
	"backend": true,
	"model":   true,
}

// ResolvedRule is a Rule with its backend looked up and its target model
// decided — everything the proxy needs, with no further config lookups.
type ResolvedRule struct {
	Match string
	// Model is the model to put on the wire, or empty to leave the agent's
	// own model in place (a pure change of destination).
	Model string
	// Backend is the destination, or nil for the agent's default upstream.
	Backend *Backend
}

// Rerouted reports whether this rule changes the destination host.
func (r ResolvedRule) Rerouted() bool { return r.Backend != nil }

// String renders the rule the way `config show` and `--dry-run` print it.
func (r ResolvedRule) String() string {
	model := r.Model
	if model == "" {
		model = "(unchanged)"
	}
	if r.Backend == nil {
		return fmt.Sprintf("%s → %s", r.Match, model)
	}
	return fmt.Sprintf("%s → %s @ %s (%s)", r.Match, model, r.Backend.Name, r.Backend.BaseURL)
}

// applyRoutes overlays a layer's [[route]] list onto e.
//
// A layer that declares any rules replaces the list wholesale rather than
// appending to it. Rules are ordered and first-match-wins, so a merged list
// would need a defined cross-layer order, and there is no ordering between
// "the rule you wrote in ~/.omni/omni.conf" and "the rule you wrote in this
// repo's ./.omni.conf" that a reader could predict. Replacing keeps the
// list you are looking at the list that runs.
func applyRoutes(e *Effective, raw []rawRoute, loc locator, issues *[]Issue) {
	if len(raw) == 0 {
		return
	}
	rules := make([]Rule, 0, len(raw))
	for i, rr := range raw {
		path := fmt.Sprintf("route[%d]", i)
		source := loc("route")

		if rr.Match == nil || *rr.Match == "" {
			*issues = append(*issues, Issue{
				Path:    path,
				Message: "rule has no match pattern",
				Source:  source,
				Level:   LevelError,
			})
			continue
		}
		r := Rule{Match: *rr.Match, Source: source}
		if rr.Backend != nil {
			r.Backend = *rr.Backend
		}
		if rr.Model != nil {
			r.Model = *rr.Model
		}
		if r.Backend == "" && r.Model == "" {
			*issues = append(*issues, Issue{
				Path: path,
				Message: fmt.Sprintf(
					"rule %q sets neither backend nor model, so it would do nothing", r.Match,
				),
				Source: source,
				Level:  LevelError,
			})
			continue
		}
		rules = append(rules, r)
	}
	e.Routes = Value[[]Rule]{rules, loc("route")}
}

// Resolve turns the rule list into ResolvedRules, looking each backend up
// and deciding each target model. It returns an Issue for every rule that
// cannot be resolved, plus a warning for any rule an earlier rule shadows.
//
// agentStyle is the APIStyle of the agent being launched, used to enforce
// the routing-not-translation rule. It is pure: callers decide whether the
// Issues are fatal.
func (e *Effective) Resolve(agentStyle string) ([]ResolvedRule, []Issue) {
	var (
		out    []ResolvedRule
		issues []Issue
	)
	for i, r := range e.Routes.V {
		path := fmt.Sprintf("route[%d]", i)
		rr := ResolvedRule{Match: r.Match, Model: r.Model}

		if r.Backend != "" {
			b, ok := e.Backends.V[r.Backend]
			if !ok {
				issues = append(issues, Issue{
					Path: path,
					Message: fmt.Sprintf(
						"unknown backend %q — declare it as [backends.%s] in omni.conf%s",
						r.Backend, r.Backend, didYouMeanBackend(r.Backend, e.Backends.V),
					),
					Source: r.Source,
					Level:  LevelError,
				})
				continue
			}
			// Routing, not translation: a backend speaking a different wire
			// format needs a converting gateway in front of it, and omni
			// must never forward a body to a URL shaped for another API and
			// hope. See internal-docs/04-interception.md §Cross-provider.
			if agentStyle != "" && b.APIStyle != agentStyle {
				issues = append(issues, Issue{
					Path: path,
					Message: fmt.Sprintf(
						"backend %q speaks %q but agent %q speaks %q — omni routes, it does not "+
							"translate; point base_url at a translating gateway that accepts %s",
						r.Backend, b.APIStyle, e.Agent, agentStyle, agentStyle,
					),
					Source: b.Source,
					Level:  LevelError,
				})
				continue
			}
			if rr.Model == "" {
				rr.Model = b.Model
			}
			rr.Backend = &b
		}
		out = append(out, rr)
	}

	issues = append(issues, shadowedRules(e.Routes.V)...)
	return out, issues
}

// shadowedRules warns about a rule that can never fire because an earlier
// rule already matches everything it would.
//
// Deciding whether one glob's language is a subset of another's is more
// work than this is worth, so the test is a sound approximation: rule j
// shadows rule i when j's pattern matches i's pattern *read as a literal
// string*. That catches every realistic mistake — a leading "*" swallowing
// the list, or "claude-*" placed above "claude-opus-5" — and stays quiet on
// patterns that merely overlap in part. It is a warning either way, so an
// approximation costs a reader a second, not a broken launch.
func shadowedRules(rules []Rule) []Issue {
	var issues []Issue
	for i := range rules {
		for j := 0; j < i; j++ {
			if !glob.Match(rules[j].Match, rules[i].Match) {
				continue
			}
			issues = append(issues, Issue{
				Path: fmt.Sprintf("route[%d]", i),
				Message: fmt.Sprintf(
					"rule %q can never match: rule %d (%q) is earlier and already matches everything it would",
					rules[i].Match, j, rules[j].Match,
				),
				Source: rules[i].Source,
				Level:  LevelWarning,
			})
			break
		}
	}
	return issues
}
