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

	Record rawRecord `toml:"record"`
}

// rawRecord mirrors the [record] sub-table. It is a value, not a pointer,
// because the decoder only ever needs "did this file set record.enabled" —
// which the inner pointer already answers — and because TestSchemaIsConsistent
// walks nested structs to derive the dotted paths every derivation must know.
type rawRecord struct {
	Enabled *bool `toml:"enabled"`
}

// rawAgent mirrors the [agents.<name>] table in omni.conf. The project
// layer (./.omni.conf) decodes into the same shape, with its keys at the
// top level and filtered down to the allowlist — see loadProjectConfig.
type rawAgent struct {
	Mode     *string `toml:"mode"`
	Redact   *bool   `toml:"redact"`
	Binary   *string `toml:"binary"`
	Upstream *string `toml:"upstream"`
	// ListenPort pins omni's loopback port for this agent. Only the port is
	// configurable: the host stays 127.0.0.1, so this cannot put a
	// credential-bearing proxy on the network.
	ListenPort *int       `toml:"listen_port"`
	Route      []rawRoute `toml:"route"`

	Record rawRecord `toml:"record"`

	Env map[string]string `toml:"env"`
}
