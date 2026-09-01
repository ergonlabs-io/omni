package config

import "strings"

// knownDefaultsPaths are the recognized dotted keys under omni.conf's
// [defaults] table (excluding the nested [agents.*] tables, which are
// validated against knownAgentPaths instead).
var knownDefaultsPaths = map[string]bool{
	"mode":                     true,
	"all_traffic":              true,
	"record.enabled":           true,
	"record.redact":            true,
	"record.bodies":            true,
	"record.retention":         true,
	"adapt.on_unrepresentable": true,
	"adapt.report_changes":     true,
	"proxy.listen":             true,
	"proxy.idle_timeout":       true,
}

// knownAgentPaths are the recognized dotted keys for an agent-shaped config:
// either omni.conf's [agents.<name>] table or an
// ~/.omni/agents/<name>.conf drop-in. route and env are wildcards — a
// [[route]] element's keys are validated separately (knownRoutePaths), and
// anything nested under env is a user-chosen variable name, not schema.
var knownAgentPaths = map[string]bool{
	"mode":                     true,
	"binary":                   true,
	"upstream":                 true,
	"adapt.on_unrepresentable": true,
	"adapt.report_changes":     true,
	"record.enabled":           true,
	"record.redact":            true,
	"record.bodies":            true,
	"record.retention":         true,
}

var knownAgentWildcards = []string{"route", "env"}

// projectAllowedPaths is the SECURITY-CRITICAL allowlist for ./.omni.conf
// (project, repo-local config). Anything not covered here is rejected with
// a warning, no matter what it's called. See
// internal-docs/08-configuration.md §Security: a repo-local file must never
// be able to set `binary` (arbitrary code execution on cd), `upstream`,
// `all_traffic`, `record.redact`, or `proxy.listen`.
func projectAllowedPath(path string) bool {
	switch {
	case path == "mode":
		return true
	case path == "route" || strings.HasPrefix(path, "route."):
		return true
	case path == "record.bodies":
		return true
	default:
		return false
	}
}

// knownPath reports whether dotted path p is recognized against exact,
// treating any path under one of wildcards as recognized too (used for
// route.* and env.*, whose leaves are validated elsewhere or are
// user-defined keys rather than schema).
func knownPath(p string, exact map[string]bool, wildcards []string) bool {
	if exact[p] {
		return true
	}
	for _, w := range wildcards {
		if p == w || strings.HasPrefix(p, w+".") {
			return true
		}
	}
	return false
}

// suggest returns the known path(s) within Levenshtein distance 2 of got,
// for "did you mean" messages. known is the same map passed to knownPath.
func suggest(got string, known map[string]bool) []string {
	var out []string
	for k := range known {
		if levenshtein(got, k) <= 2 {
			out = append(out, k)
		}
	}
	return out
}

// levenshtein computes edit distance between a and b. A small local copy —
// internal/profile has an equivalent but unexported implementation, and
// duplicating ~20 lines is cheaper than coupling config's validation to
// profile's internals.
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
			del := prev[j] + 1
			ins := cur[j-1] + 1
			sub := prev[j-1] + cost
			m := del
			if ins < m {
				m = ins
			}
			if sub < m {
				m = sub
			}
			cur[j] = m
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
