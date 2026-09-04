package config

import "fmt"

// locator returns the provenance string for a dotted, layer-local path
// (e.g. "mode", "record.enabled") — typically "file:line" via sourceAt, or a
// fixed label for non-file layers (env, CLI overrides).
type locator func(path string) string

// applyDefaults overlays r (an omni.conf [defaults] table, or an
// environment-sourced equivalent) onto e, recording provenance via loc and
// appending any validation problems to *issues.
//
// Only fields the layer actually set are applied — that is what makes this a
// deep merge rather than a whole-file replace, and the pointer fields on the
// raw structs are how "set" is told apart from "set to the zero value".
func applyDefaults(e *Effective, r rawDefaults, loc locator, issues *[]Issue) {
	if r.Mode != nil {
		applyMode(e, *r.Mode, loc("mode"), issues)
	}
	if r.Redact != nil {
		e.Redact = Value[bool]{*r.Redact, loc("redact")}
	}
	if r.Record.Enabled != nil {
		e.RecordEnabled = Value[bool]{*r.Record.Enabled, loc("record.enabled")}
	}
}

// applyAgent overlays r (an [agents.X] table, an environment-sourced
// equivalent, or the filtered result of a project config) onto e.
//
// binary and upstream are absent from rawDefaults and so appear only here:
// they name one agent's executable and endpoint, which is not a thing a
// global default can meaningfully say.
func applyAgent(e *Effective, r rawAgent, loc locator, issues *[]Issue) {
	if r.Mode != nil {
		applyMode(e, *r.Mode, loc("mode"), issues)
	}
	if r.Redact != nil {
		e.Redact = Value[bool]{*r.Redact, loc("redact")}
	}
	if r.Record.Enabled != nil {
		e.RecordEnabled = Value[bool]{*r.Record.Enabled, loc("record.enabled")}
	}
	if r.Binary != nil {
		e.Binary = Value[string]{*r.Binary, loc("binary")}
	}
	if r.Upstream != nil {
		e.Upstream = Value[string]{*r.Upstream, loc("upstream")}
	}
	if r.ListenPort != nil {
		applyListenPort(e, *r.ListenPort, loc("listen_port"), issues)
	}
	applyRoutes(e, r.Route, loc, issues)
	applyEnv(e, r.Env, loc)
}

// applyMode validates and stores a mode. A bad value is a LevelError and
// leaves the previously resolved mode in place: a typo in a high layer must
// not silently zero out a good value from a lower one.
func applyMode(e *Effective, raw, source string, issues *[]Issue) {
	m, err := convMode(raw)
	if err != nil {
		*issues = append(*issues, Issue{
			Path:    "mode",
			Message: err.Error(),
			Source:  source,
			Level:   LevelError,
		})
		return
	}
	e.Mode = Value[Mode]{m, source}
}

// applyEnv merges r into e's child-process environment. Unlike every other
// key this accumulates across layers rather than replacing: a project config
// adding one variable should not drop the ones the global config set.
func applyEnv(e *Effective, r map[string]string, loc locator) {
	if len(r) == 0 {
		return
	}
	merged := make(map[string]string, len(e.Env.V)+len(r))
	for k, v := range e.Env.V {
		merged[k] = v
	}
	for k, v := range r {
		merged[k] = v
	}
	e.Env = Value[map[string]string]{merged, loc("env")}
}

// applyListenPort validates a pinned proxy port.
//
// Only the port is configurable, never the host: the bind stays on
// 127.0.0.1, so no setting here can expose a proxy that forwards a
// credential to the network. Ports below 1024 are refused because omni does
// not run privileged and binding one would fail at launch with a far less
// obvious message than this.
func applyListenPort(e *Effective, port int, source string, issues *[]Issue) {
	if port < 1024 || port > 65535 {
		*issues = append(*issues, Issue{
			Path:    "listen_port",
			Message: fmt.Sprintf("listen_port %d is out of range (want 1024-65535)", port),
			Source:  source,
			Level:   LevelError,
		})
		return
	}
	e.ListenPort = Value[int]{port, source}
}
