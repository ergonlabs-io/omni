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

// rawDefaults mirrors omni.conf's [defaults] table plus its subtables.
type rawDefaults struct {
	Mode       *string `toml:"mode"`
	AllTraffic *bool   `toml:"all_traffic"`

	Record rawRecord `toml:"record"`
	Adapt  rawAdapt  `toml:"adapt"`
	Proxy  rawProxy  `toml:"proxy"`
}

// rawRecord mirrors a [*.record] table.
type rawRecord struct {
	Enabled   *bool   `toml:"enabled"`
	Redact    *bool   `toml:"redact"`
	Bodies    *bool   `toml:"bodies"`
	Retention *string `toml:"retention"`
}

// rawAdapt mirrors a [*.adapt] table.
type rawAdapt struct {
	OnUnrepresentable *string `toml:"on_unrepresentable"`
	ReportChanges     *bool   `toml:"report_changes"`
}

// rawProxy mirrors [defaults.proxy]. Global only — never per-agent.
type rawProxy struct {
	Listen      *string `toml:"listen"`
	IdleTimeout *string `toml:"idle_timeout"`
}

// rawAgent mirrors both the inline [agents.<name>] table in omni.conf and
// the shape of a drop-in ~/.omni/agents/<name>.conf file (which is the same
// fields at the top level, without an [agents.<name>] wrapper).
type rawAgent struct {
	Mode     *string    `toml:"mode"`
	Binary   *string    `toml:"binary"`
	Upstream *string    `toml:"upstream"`
	Route    []rawRoute `toml:"route"`

	Adapt  rawAdapt          `toml:"adapt"`
	Record rawRecord         `toml:"record"`
	Env    map[string]string `toml:"env"`
}
