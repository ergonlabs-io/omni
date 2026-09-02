package config

import "fmt"

// Mode controls how much omni does to a session's intercepted traffic.
//
// There is deliberately no separate "route" mode: routing is on whenever
// [[route]] rules exist and Mode is not off. A rule you wrote is a rule you
// meant, and a third mode would only add a state where rules are configured,
// valid, shown by `config show`, and silently doing nothing.
type Mode string

const (
	// ModeOff is passthrough: forward only. Nothing is recorded and no rule
	// is applied.
	ModeOff Mode = "off"
	// ModeRecord captures all traffic to ~/.omni/sessions, and applies any
	// [[route]] rules. Default.
	ModeRecord Mode = "record"
)

// validModes is used both for enum validation and for building a readable
// error message.
var validModes = []Mode{ModeOff, ModeRecord}

func (m Mode) valid() bool {
	for _, v := range validModes {
		if m == v {
			return true
		}
	}
	return false
}

// convMode validates the mode enum.
func convMode(s string) (Mode, error) {
	m := Mode(s)
	if !m.valid() {
		return "", fmt.Errorf("invalid mode %q (want %q or %q)", s, ModeOff, ModeRecord)
	}
	return m, nil
}

// Level classifies a validation Issue by what it does to the run. The three
// values are ordered by severity, so a comparison is enough to ask "is this
// at least an error?".
type Level int

const (
	// LevelWarning is reported and otherwise ignored: it does not fail
	// `omni config check` and does not stop a launch.
	LevelWarning Level = iota
	// LevelError fails `omni config check` with a nonzero exit and aborts a
	// launch before the agent starts, but still lets Load return a usable
	// configuration for `config show` to report.
	LevelError
	// LevelFatal additionally refuses to hand back a usable configuration
	// at all: Load and Override both return an error. Reserved for the one
	// category internal-docs/08-configuration.md §Security calls out — a
	// credential-shaped value anywhere in config.
	LevelFatal
)

func (l Level) String() string {
	switch l {
	case LevelFatal:
		return "fatal"
	case LevelError:
		return "error"
	default:
		return "warning"
	}
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

	Mode Value[Mode]
	// Redact strips credential headers (Authorization, x-api-key,
	// *-api-key) from recorded traffic. Only meaningful when Mode records.
	Redact Value[bool]
	// Binary overrides the agent's profile.Binary when V is non-empty.
	Binary Value[string]
	// Upstream overrides the agent's profile.Upstream when V is non-empty.
	Upstream Value[string]

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
	// (credential scan, route resolution). Unlike Issues these
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

// HasFatal reports whether any accumulated Issue is LevelFatal — a
// credential-shaped value anywhere in config.
func (e *Effective) HasFatal() bool { return e.hasAtLeast(LevelFatal) }

// HasErrors reports whether any accumulated Issue is LevelError or worse.
// Used by `omni config check` to decide its exit code.
func (e *Effective) HasErrors() bool { return e.hasAtLeast(LevelError) }

func (e *Effective) hasAtLeast(l Level) bool {
	for _, is := range e.allIssues() {
		if is.Level >= l {
			return true
		}
	}
	return false
}
