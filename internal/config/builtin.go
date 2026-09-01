package config

import "time"

// builtinSource labels layer 1 (compiled-in defaults) in provenance output.
const builtinSource = "(built-in default)"

// mustParseDuration panics on error — used only for constants we control
// ourselves (built-in defaults), where a parse failure is a bug in this
// package, not bad user input.
func mustParseDuration(s string) time.Duration {
	d, err := ParseDuration(s)
	if err != nil {
		panic("config: bad built-in duration " + s + ": " + err.Error())
	}
	return d
}

// builtinDefaults returns layer 1: the compiled-in defaults, matching the
// values documented and generated into omni.conf by Init. See
// internal-docs/08-configuration.md §Global config.
func builtinDefaults(agent string) *Effective {
	e := &Effective{Agent: agent}

	e.Mode = Value[Mode]{ModeRecord, builtinSource}
	e.AllTraffic = Value[bool]{false, builtinSource}
	// Binary and Upstream are unset by default: the agent's profile.Profile
	// supplies them unless config overrides.
	e.Binary = Value[string]{"", ""}
	e.Upstream = Value[string]{"", ""}

	e.Record.Enabled = Value[bool]{true, builtinSource}
	e.Record.Redact = Value[bool]{true, builtinSource}
	e.Record.Bodies = Value[bool]{true, builtinSource}
	e.Record.Retention = Value[Duration]{Duration(mustParseDuration("14d")), builtinSource}

	e.Adapt.OnUnrepresentable = Value[string]{"error", builtinSource}
	e.Adapt.ReportChanges = Value[bool]{true, builtinSource}

	e.Proxy.Listen = Value[string]{"127.0.0.1:0", builtinSource}
	e.Proxy.IdleTimeout = Value[Duration]{Duration(mustParseDuration("10m")), builtinSource}

	e.ModelMap = Value[map[string]string]{map[string]string{}, builtinSource}
	e.Env = Value[map[string]string]{map[string]string{}, builtinSource}

	return e
}
