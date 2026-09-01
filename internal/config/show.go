package config

import (
	"fmt"
	"sort"
	"strings"
)

// Row is one printable line of `omni config show`: a dotted key, its
// effective value (already formatted as it should appear in the table),
// and where it came from.
type Row struct {
	Path   string
	Value  string
	Source string
}

// Rows renders every effective value as a Row, in a stable, readable order.
// Unset optional fields (binary/upstream overrides, no routes, no
// backends, an empty env) are omitted rather than shown as empty strings.
func (e *Effective) Rows() []Row {
	var rows []Row
	add := func(path, val, source string) { rows = append(rows, Row{path, val, source}) }

	add("mode", fmt.Sprintf("%q", string(e.Mode.V)), e.Mode.Source)
	add("all_traffic", fmt.Sprintf("%v", e.AllTraffic.V), e.AllTraffic.Source)
	if e.Binary.V != "" {
		add("binary", fmt.Sprintf("%q", e.Binary.V), e.Binary.Source)
	}
	if e.Upstream.V != "" {
		add("upstream", fmt.Sprintf("%q", e.Upstream.V), e.Upstream.Source)
	}
	add("record.enabled", fmt.Sprintf("%v", e.Record.Enabled.V), e.Record.Enabled.Source)
	add("record.redact", fmt.Sprintf("%v", e.Record.Redact.V), e.Record.Redact.Source)
	add("record.bodies", fmt.Sprintf("%v", e.Record.Bodies.V), e.Record.Bodies.Source)
	add("record.retention", fmt.Sprintf("%q", e.Record.Retention.V.String()), e.Record.Retention.Source)
	add("adapt.on_unrepresentable", fmt.Sprintf("%q", e.Adapt.OnUnrepresentable.V), e.Adapt.OnUnrepresentable.Source)
	add("adapt.report_changes", fmt.Sprintf("%v", e.Adapt.ReportChanges.V), e.Adapt.ReportChanges.Source)
	add("proxy.listen", fmt.Sprintf("%q", e.Proxy.Listen.V), e.Proxy.Listen.Source)
	add("proxy.idle_timeout", fmt.Sprintf("%q", e.Proxy.IdleTimeout.V.String()), e.Proxy.IdleTimeout.Source)
	for _, name := range sortedBackendNames(e.Backends.V) {
		b := e.Backends.V[name]
		add("backends."+name, fmt.Sprintf("%s (%s, $%s)", b.BaseURL, b.APIStyle, b.APIKeyEnv), b.Source)
	}
	for i, r := range e.Routes.V {
		add(fmt.Sprintf("route[%d]", i), formatRule(r), r.Source)
	}
	if len(e.Env.V) > 0 {
		add("env", formatKVMap(e.Env.V), e.Env.Source)
	}
	return rows
}

// Show renders Rows as an aligned, provenance-annotated table, matching the
// shape of the `omni config show` example in
// internal-docs/08-configuration.md.
func (e *Effective) Show() string {
	rows := e.Rows()
	wPath, wVal := 0, 0
	for _, r := range rows {
		if len(r.Path) > wPath {
			wPath = len(r.Path)
		}
		if len(r.Value) > wVal {
			wVal = len(r.Value)
		}
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%-*s  %-*s  %s\n", wPath, r.Path, wVal, r.Value, r.Source)
	}
	return b.String()
}

// formatRule renders a rule as written, before backend resolution — Rows
// reports configuration, and Resolve reports what it turns into.
func formatRule(r Rule) string {
	switch {
	case r.Backend != "" && r.Model != "":
		return fmt.Sprintf("%s → %s @ %s", r.Match, r.Model, r.Backend)
	case r.Backend != "":
		return fmt.Sprintf("%s → @%s", r.Match, r.Backend)
	default:
		return fmt.Sprintf("%s → %s", r.Match, r.Model)
	}
}

func formatArrowMap(m map[string]string) string {
	keys := sortedKeys(m)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s → %s", k, m[k])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func formatKVMap(m map[string]string) string {
	keys := sortedKeys(m)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%s", k, m[k])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
