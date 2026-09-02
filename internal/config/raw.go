package config

// This file defines the Go shapes that mirror omni.conf's TOML layout
// exactly, for use with github.com/BurntSushi/toml. Every field is a
// pointer (or, for maps, left nil) so "not present in this file" is
// distinguishable from "present with the zero value" — that distinction is
// what makes per-key provenance and deep merge possible.

// rawGlobal mirrors the top level of ~/.omni/omni.conf.
type rawGlobal struct {
	Defaults rawDefaults           `toml:"defaults"`
	Agents   map[string]rawAgent   `toml:"agents"`
	Backends map[string]rawBackend `toml:"backends"`
}

// rawDefaults mirrors omni.conf's [defaults] table.
type rawDefaults struct {
	Mode   *string `toml:"mode"`
	Redact *bool   `toml:"redact"`
}

// rawAgent mirrors the [agents.<name>] table in omni.conf. The project
// layer (./.omni.conf) decodes into the same shape, with its keys at the
// top level and filtered down to the allowlist — see loadProjectConfig.
type rawAgent struct {
	Mode     *string    `toml:"mode"`
	Redact   *bool      `toml:"redact"`
	Binary   *string    `toml:"binary"`
	Upstream *string    `toml:"upstream"`
	Route    []rawRoute `toml:"route"`

	Env map[string]string `toml:"env"`
}
