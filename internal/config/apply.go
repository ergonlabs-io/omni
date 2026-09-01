package config

import "fmt"

// locator returns the provenance string for a dotted, layer-local path
// (e.g. "mode", "record.bodies") — typically "file:line" via sourceAt, or a
// fixed label for non-file layers (env, CLI overrides).
type locator func(path string) string

// applyDefaults overlays r (an omni.conf [defaults] table, or an
// environment-sourced equivalent) onto e, recording provenance via loc and
// appending any validation problems to *issues. Only non-nil fields in r
// are applied — that is what makes this a deep merge rather than a
// whole-file replace.
func applyDefaults(e *Effective, r rawDefaults, loc locator, issues *[]Issue) {
	if r.Mode != nil {
		applyMode(&e.Mode, *r.Mode, loc("mode"), issues)
	}
	if r.AllTraffic != nil {
		e.AllTraffic = Value[bool]{*r.AllTraffic, loc("all_traffic")}
	}
	applyRecord(&e.Record, r.Record, loc, issues)
	applyAdapt(&e.Adapt, r.Adapt, loc, issues)
	if r.Proxy.Listen != nil {
		e.Proxy.Listen = Value[string]{*r.Proxy.Listen, loc("proxy.listen")}
	}
	if r.Proxy.IdleTimeout != nil {
		applyDuration(&e.Proxy.IdleTimeout, *r.Proxy.IdleTimeout, "proxy.idle_timeout", loc("proxy.idle_timeout"), issues)
	}
}

// applyAgent overlays r (an inline [agents.X] table, an
// agents/<name>.conf drop-in, or the filtered result of a project config)
// onto e. Proxy settings are deliberately absent from rawAgent: a single
// omni process has one proxy, so there is no per-agent proxy to overlay.
func applyAgent(e *Effective, r rawAgent, loc locator, issues *[]Issue) {
	if r.Mode != nil {
		applyMode(&e.Mode, *r.Mode, loc("mode"), issues)
	}
	if r.Binary != nil {
		e.Binary = Value[string]{*r.Binary, loc("binary")}
	}
	if r.Upstream != nil {
		e.Upstream = Value[string]{*r.Upstream, loc("upstream")}
	}
	applyRoutes(e, r.Route, loc, issues)
	if len(r.Env) > 0 {
		merged := make(map[string]string, len(e.Env.V)+len(r.Env))
		for k, v := range e.Env.V {
			merged[k] = v
		}
		for k, v := range r.Env {
			merged[k] = v
		}
		e.Env = Value[map[string]string]{merged, loc("env")}
	}
	applyRecord(&e.Record, r.Record, loc, issues)
	applyAdapt(&e.Adapt, r.Adapt, loc, issues)
}

func applyRecord(dst *RecordEffective, r rawRecord, loc locator, issues *[]Issue) {
	if r.Enabled != nil {
		dst.Enabled = Value[bool]{*r.Enabled, loc("record.enabled")}
	}
	if r.Redact != nil {
		dst.Redact = Value[bool]{*r.Redact, loc("record.redact")}
	}
	if r.Bodies != nil {
		dst.Bodies = Value[bool]{*r.Bodies, loc("record.bodies")}
	}
	if r.Retention != nil {
		applyDuration(&dst.Retention, *r.Retention, "record.retention", loc("record.retention"), issues)
	}
}

func applyAdapt(dst *AdaptEffective, r rawAdapt, loc locator, issues *[]Issue) {
	if r.OnUnrepresentable != nil {
		v := *r.OnUnrepresentable
		if v != "error" && v != "warn" {
			*issues = append(*issues, Issue{
				Path:    "adapt.on_unrepresentable",
				Message: fmt.Sprintf("invalid value %q (want \"error\" or \"warn\")", v),
				Source:  loc("adapt.on_unrepresentable"),
				Level:   LevelError,
			})
		} else {
			dst.OnUnrepresentable = Value[string]{v, loc("adapt.on_unrepresentable")}
		}
	}
	if r.ReportChanges != nil {
		dst.ReportChanges = Value[bool]{*r.ReportChanges, loc("adapt.report_changes")}
	}
}

func applyMode(dst *Value[Mode], raw, source string, issues *[]Issue) {
	m := Mode(raw)
	if !m.valid() {
		*issues = append(*issues, Issue{
			Path:    "mode",
			Message: fmt.Sprintf("invalid mode %q (want \"off\", \"record\", or \"route\")", raw),
			Source:  source,
			Level:   LevelError,
		})
		return
	}
	*dst = Value[Mode]{m, source}
}

func applyDuration(dst *Value[Duration], raw, path, source string, issues *[]Issue) {
	d, err := ParseDuration(raw)
	if err != nil {
		*issues = append(*issues, Issue{
			Path:    path,
			Message: fmt.Sprintf("invalid duration %q: %s", raw, err),
			Source:  source,
			Level:   LevelError,
		})
		return
	}
	*dst = Value[Duration]{Duration(d), source}
}
