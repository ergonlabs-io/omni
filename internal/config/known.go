package config

import (
	"maps"
	"slices"
	"strings"

	"github.com/ergonlabs-io/omni/internal/suggest"
)

// knownDefaultsPaths and knownAgentPaths are the recognized keys for
// omni.conf's [defaults] table and for an agent-shaped config (an
// [agents.<name>] table, or a project config's top level).
//
// These are hand-maintained, and TestSchemaIsConsistent is what keeps them
// honest: it reflects over rawDefaults and rawAgent and fails if a key
// exists on the wire but is missing from these sets, from the environment
// setters, or from `config show`. Each of those omissions fails silently in
// production — a key absent here is reported to the user as a typo — so the
// test, not the reader, is the guarantee.
var knownDefaultsPaths = map[string]bool{
	"mode":           true,
	"redact":         true,
	"record.enabled": true,
}

var knownAgentPaths = map[string]bool{
	"mode":           true,
	"redact":         true,
	"record.enabled": true,
	"binary":         true,
	"upstream":       true,
	"listen_port":    true,
}

// knownAgentWildcards are the agent-shaped keys whose leaves are not
// schema: a [[route]] element's keys are validated separately against
// knownRoutePaths, and anything under env is a user-chosen variable name.
var knownAgentWildcards = []string{"route", "env"}

// projectAllowedPath is the SECURITY-CRITICAL allowlist for ./.omni.conf
// (project, repo-local config): a repo you cd into and did not write must
// never be able to set `binary` (arbitrary code execution on cd),
// `upstream`, `all_traffic`, `redact`, `record.enabled`, or `proxy.listen`.
// record.enabled belongs on that list twice over: a repo that could switch
// recording on would be choosing to write your prompts — its own source
// included — to disk on your behalf.
// See internal-docs/08-configuration.md §Security, and read
// loadProjectConfig before adding anything here. TestProjectScopeIsMinimal
// pins the list.
//
// route is allowed here and is not in the schema table; it is admitted in
// its rename form only, which assignProjectRoutes enforces.
func projectAllowedPath(path string) bool {
	if path == "route" || strings.HasPrefix(path, "route.") {
		return true
	}
	return path == "mode"
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

// suggestNear returns the known path(s) close enough to got to be worth
// offering as a correction. known is the same map passed to knownPath.
func suggestNear(got string, known map[string]bool) []string {
	return suggest.Near(got, slices.Sorted(maps.Keys(known)))
}
