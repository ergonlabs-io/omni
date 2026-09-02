package config

// builtinSource labels layer 1 (compiled-in defaults) in provenance output.
const builtinSource = "(built-in default)"

// builtinDefaults returns layer 1: the compiled-in defaults. These must match
// the values `omni init` writes into omni.conf — see
// internal-docs/08-configuration.md §Global config.
func builtinDefaults(agent string) *Effective {
	e := &Effective{Agent: agent}

	e.Mode = Value[Mode]{ModeRecord, builtinSource}
	// Recording is opt-in. See Effective.RecordEnabled for why this default
	// is not symmetric with Mode's.
	e.RecordEnabled = Value[bool]{false, builtinSource}
	e.Redact = Value[bool]{true, builtinSource}

	// Binary and Upstream are unset by default: the agent's profile.Profile
	// supplies them unless config overrides. The empty source is what makes
	// `config show` omit them rather than print an empty override.
	e.Binary = Value[string]{"", ""}
	e.Upstream = Value[string]{"", ""}

	e.Routes = Value[[]Rule]{nil, builtinSource}
	e.Backends = Value[map[string]Backend]{map[string]Backend{}, builtinSource}
	e.Env = Value[map[string]string]{map[string]string{}, builtinSource}

	return e
}
