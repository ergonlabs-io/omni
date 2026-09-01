package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// envLayers scans the process environment for OMNI_* variables and splits
// them into two patches for agent: one that behaves like omni.conf's
// [defaults] table (unscoped keys, e.g. OMNI_MODE, OMNI_RECORD__REDACT),
// and one that behaves like an [agents.<agent>] table
// (OMNI_AGENTS__<AGENT>__..., matched case-insensitively against agent;
// env vars scoped to a different agent are ignored here).
//
// Nesting uses "__" (e.g. OMNI_RECORD__REDACT -> record.redact); a single
// "_" is part of a key's own name (e.g. all_traffic, idle_timeout) and is
// never treated as a separator, matching every multi-word key in the TOML
// schema. route and env (the list- and map-valued fields) cannot be set this
// way — model names routinely contain characters environment variable
// naming can't carry unambiguously — and produce a warning Issue if
// attempted.
//
// The two returned source maps record, per local dotted path successfully
// applied, the exact "$ENV_VAR_NAME" that set it — used for provenance in
// `omni config show`.
func envLayers(agent string) (d rawDefaults, a rawAgent, srcDefaults, srcAgent map[string]string, issues []Issue) {
	srcDefaults = map[string]string{}
	srcAgent = map[string]string{}
	agentLower := strings.ToLower(agent)

	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == HomeEnvVar || !strings.HasPrefix(k, "OMNI_") {
			continue
		}
		rest := strings.TrimPrefix(k, "OMNI_")
		if rest == "" {
			continue
		}
		segs := strings.Split(rest, "__")
		for i := range segs {
			segs[i] = strings.ToLower(segs[i])
		}
		envSrc := "$" + k

		if segs[0] == "agents" {
			if len(segs) < 3 {
				continue // e.g. bare OMNI_AGENTS__CLAUDE — nothing to set
			}
			name, local := segs[1], strings.Join(segs[2:], ".")
			if name != agentLower {
				continue // scoped to a different agent
			}
			known, err := setRawAgentField(&a, local, v)
			if known && err == nil {
				srcAgent[local] = envSrc
			}
			issues = append(issues, envIssue(k, "agents."+name+"."+local, envSrc, local, known, err)...)
			continue
		}

		local := strings.Join(segs, ".")
		known, err := setRawDefaultsField(&d, local, v)
		if known && err == nil {
			srcDefaults[local] = envSrc
		}
		issues = append(issues, envIssue(k, local, envSrc, local, known, err)...)
	}
	return d, a, srcDefaults, srcAgent, issues
}

func envIssue(envVar, path, source, local string, known bool, err error) []Issue {
	if known && err == nil {
		return nil
	}
	if !known {
		return []Issue{{
			Path: path,
			Message: fmt.Sprintf(
				"environment variable %s does not map to a known config key %q (route and env cannot be set via environment variables)",
				envVar, local,
			),
			Source: source,
			Level:  LevelWarning,
		}}
	}
	return []Issue{{
		Path:    path,
		Message: fmt.Sprintf("environment variable %s: %s", envVar, err),
		Source:  source,
		Level:   LevelError,
	}}
}

// setRawDefaultsField sets the field of d named by the dotted, defaults-
// relative path (e.g. "record.redact") to v, coercing to the field's type.
// known reports whether path names a real field; err reports a type
// coercion failure (e.g. a non-boolean value for a bool field).
func setRawDefaultsField(d *rawDefaults, path, v string) (known bool, err error) {
	switch path {
	case "mode":
		d.Mode = &v
	case "all_traffic":
		b, e := strconv.ParseBool(v)
		if e != nil {
			return true, e
		}
		d.AllTraffic = &b
	case "record.enabled":
		b, e := strconv.ParseBool(v)
		if e != nil {
			return true, e
		}
		d.Record.Enabled = &b
	case "record.redact":
		b, e := strconv.ParseBool(v)
		if e != nil {
			return true, e
		}
		d.Record.Redact = &b
	case "record.bodies":
		b, e := strconv.ParseBool(v)
		if e != nil {
			return true, e
		}
		d.Record.Bodies = &b
	case "record.retention":
		d.Record.Retention = &v
	case "adapt.on_unrepresentable":
		d.Adapt.OnUnrepresentable = &v
	case "adapt.report_changes":
		b, e := strconv.ParseBool(v)
		if e != nil {
			return true, e
		}
		d.Adapt.ReportChanges = &b
	case "proxy.listen":
		d.Proxy.Listen = &v
	case "proxy.idle_timeout":
		d.Proxy.IdleTimeout = &v
	default:
		return false, nil
	}
	return true, nil
}

// setRawAgentField is setRawDefaultsField's counterpart for agent-shaped
// (per-agent) keys.
func setRawAgentField(a *rawAgent, path, v string) (known bool, err error) {
	switch path {
	case "mode":
		a.Mode = &v
	case "binary":
		a.Binary = &v
	case "upstream":
		a.Upstream = &v
	case "record.enabled":
		b, e := strconv.ParseBool(v)
		if e != nil {
			return true, e
		}
		a.Record.Enabled = &b
	case "record.redact":
		b, e := strconv.ParseBool(v)
		if e != nil {
			return true, e
		}
		a.Record.Redact = &b
	case "record.bodies":
		b, e := strconv.ParseBool(v)
		if e != nil {
			return true, e
		}
		a.Record.Bodies = &b
	case "record.retention":
		a.Record.Retention = &v
	case "adapt.on_unrepresentable":
		a.Adapt.OnUnrepresentable = &v
	case "adapt.report_changes":
		b, e := strconv.ParseBool(v)
		if e != nil {
			return true, e
		}
		a.Adapt.ReportChanges = &b
	default:
		return false, nil
	}
	return true, nil
}
