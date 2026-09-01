package config

import "fmt"

// Mode controls how much omni does to a session's intercepted traffic.
type Mode string

const (
	// ModeOff is passthrough: no proxy involvement beyond forwarding.
	ModeOff Mode = "off"
	// ModeRecord captures all traffic to ~/.omni/sessions. Default.
	ModeRecord Mode = "record"
	// ModeRoute records and additionally applies the routing rules and the
	// capability adapter.
	ModeRoute Mode = "route"
)

// validModes is used both for enum validation and for building a readable
// error message.
var validModes = []Mode{ModeOff, ModeRecord, ModeRoute}

func (m Mode) valid() bool {
	for _, v := range validModes {
		if m == v {
			return true
		}
	}
	return false
}

// Level classifies a validation Issue.
type Level int

const (
	// LevelWarning is reported but does not fail `omni config check`.
	LevelWarning Level = iota
	// LevelError fails `omni config check` (nonzero exit) and, for the two
	// categories called out in internal-docs/08-configuration.md §Security
	// (non-loopback proxy.listen, credential-shaped values), also fails Load.
	LevelError
)

func (l Level) String() string {
	if l == LevelError {
		return "error"
	}
	return "warning"
}

// Issue is a single problem found while loading or validating config.
type Issue struct {
	// Path is the dotted config key the issue concerns, e.g. "proxy.listen"
	// or "record.retention". Empty for issues that are not key-specific.
	Path string
	// Message is a human-readable, actionable description.
	Message string
	// Source is where the offending value came from: "file:line", a layer
	// label such as "(built-in default)" or "(env: OMNI_MODE)", or "(cli
	// flag)".
	Source string
	Level  Level
	// Fatal marks the two Load-time hard-stop categories: non-loopback
	// proxy.listen and credential-shaped values anywhere in config. Load
	// refuses to hand back a usable *Effective when any Issue has Fatal set.
	Fatal bool
}

func (i Issue) String() string {
	loc := i.Source
	if loc == "" {
		loc = "(unknown source)"
	}
	if i.Path == "" {
		return fmt.Sprintf("%s: %s [%s]", i.Level, i.Message, loc)
	}
	return fmt.Sprintf("%s: %s: %s [%s]", i.Level, i.Path, i.Message, loc)
}

// Value pairs a resolved config value with provenance: which layer set it.
type Value[T any] struct {
	V      T
	Source string
}

// Effective is the fully merged, per-agent configuration, with per-field
// provenance. It is the output of Load and the input to `omni config show`.
type Effective struct {
	// Agent is the agent name this configuration was resolved for.
	Agent string

	Mode       Value[Mode]
	AllTraffic Value[bool]
	// Binary overrides the agent's profile.Binary when V is non-empty.
	Binary Value[string]
	// Upstream overrides the agent's profile.Upstream when V is non-empty.
	Upstream Value[string]

	Record RecordEffective
	Adapt  AdaptEffective
	Proxy  ProxyEffective

	// Routes is the ordered routing rule list, first match wins. Per-agent.
	// See Resolve to pair it with Backends.
	Routes Value[[]Rule]
	// Backends are the declared destinations a rule can target, keyed by
	// name. Global config only.
	Backends Value[map[string]Backend]
	// Env is extra environment injected into the child process.
	Env Value[map[string]string]

	// Issues accumulates the problems found while *merging* layers: unknown
	// keys, bad enums, unparsable durations. Each is discovered once, when
	// the layer that caused it is read.
	Issues []Issue

	// checkIssues holds the problems derived from the fully merged state
	// (credential scan, loopback, route resolution). Unlike Issues these
	// are recomputed from scratch every time runChecks runs — Override
	// re-runs it, and a check that appended would report the same problem
	// once per invocation. See allIssues.
	checkIssues []Issue
}

// allIssues returns the merge-time and derived problems as one slice.
func (e *Effective) allIssues() []Issue {
	if len(e.checkIssues) == 0 {
		return e.Issues
	}
	out := make([]Issue, 0, len(e.Issues)+len(e.checkIssues))
	out = append(out, e.Issues...)
	return append(out, e.checkIssues...)
}

// RecordEffective is the resolved [record] section.
type RecordEffective struct {
	Enabled   Value[bool]
	Redact    Value[bool]
	Bodies    Value[bool]
	Retention Value[Duration]
}

// AdaptEffective is the resolved [adapt] section.
type AdaptEffective struct {
	// OnUnrepresentable is "error" or "warn".
	OnUnrepresentable Value[string]
	ReportChanges     Value[bool]
}

// ProxyEffective is the resolved [proxy] section. Global only — a single
// omni process has one proxy, so this is never overridden per-agent.
type ProxyEffective struct {
	Listen      Value[string]
	IdleTimeout Value[Duration]
}

// HasFatal reports whether any accumulated Issue is Fatal — the two
// Load-time hard-stop categories (non-loopback proxy.listen, a
// credential-shaped value anywhere in config).
func (e *Effective) HasFatal() bool {
	for _, is := range e.allIssues() {
		if is.Fatal {
			return true
		}
	}
	return false
}

// HasErrors reports whether any accumulated Issue is at LevelError. Used by
// `omni config check` to decide its exit code.
func (e *Effective) HasErrors() bool {
	for _, is := range e.allIssues() {
		if is.Level == LevelError {
			return true
		}
	}
	return false
}

// RoutingError returns a descriptive error if routing rules are set but the
// agent's wire style cannot be rewritten (see profile.APIStyle.CanRewrite),
// or nil otherwise. Callers (cmd/omni) can use this to fail a `route`
// launch loudly rather than silently no-op the rules, matching
// internal-docs/09-cli-design.md's example error.
func (e *Effective) RoutingError(canRewrite bool) error {
	if canRewrite || len(e.Routes.V) == 0 {
		return nil
	}
	return fmt.Errorf(
		"cannot apply routing rules for agent %q: model rewriting is not supported for this agent's API style (%s)",
		e.Agent, e.Routes.Source,
	)
}
