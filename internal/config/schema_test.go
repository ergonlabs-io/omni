package config

import (
	"reflect"
	"strings"
	"testing"
)

// The config schema is small enough to be written out longhand, which means
// the same key is named in five places: the raw structs, applyDefaults /
// applyAgent, the known-path sets, the environment setters, and Rows. Four
// of those five fail *silently* when a key is missing from them — a key
// absent from knownAgentPaths is reported to the user as a typo, one absent
// from setRawAgentField is quietly ignored by OMNI_* — so the tests here,
// not the reader, are what keep them in step.
//
// They work off reflection over the raw structs, which are the wire format,
// and drive the real functions rather than any description of them.

// nonScalarKeys are carried by the raw structs but are not scalar settings:
// their merge, validation, and display rules are per-key. See applyRoutes,
// applyEnv, and backend.go.
var nonScalarKeys = map[string]bool{"route": true, "env": true}

// probes gives each scalar key a value that is valid and different from its
// built-in default, so "it round-tripped" cannot be confused with "it was
// never touched".
var probes = map[string]string{
	"mode":           "off",
	"redact":         "false",
	"record.enabled": "true",
	"binary":         "/probe/binary",
	"upstream":       "https://probe.example",
	"listen_port":    "8787",
}

// tomlLeafPaths walks a raw config struct the way BurntSushi/toml decodes
// into it and returns every dotted key it can carry.
func tomlLeafPaths(t *testing.T, typ reflect.Type, prefix string) []string {
	t.Helper()
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("toml")
		if tag == "" || tag == "-" {
			t.Fatalf("%s.%s has no toml tag", typ.Name(), f.Name)
		}
		path := tag
		if prefix != "" {
			path = prefix + "." + tag
		}
		if f.Type.Kind() == reflect.Struct {
			out = append(out, tomlLeafPaths(t, f.Type, path)...)
			continue
		}
		out = append(out, path)
	}
	return out
}

func scalarKeys(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	var out []string
	for _, p := range tomlLeafPaths(t, typ, "") {
		if !nonScalarKeys[p] {
			out = append(out, p)
		}
	}
	return out
}

// TestSchemaIsConsistent fails if a key exists on the wire but some
// derivation has not heard about it — or if a derivation names a key the
// wire format does not have.
func TestSchemaIsConsistent(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		keys := scalarKeys(t, reflect.TypeOf(rawDefaults{}))
		for _, k := range keys {
			if !knownDefaultsPaths[k] {
				t.Errorf("rawDefaults carries %q but knownDefaultsPaths omits it — "+
					"the user would be told it is an unknown key", k)
			}
			var d rawDefaults
			if known, err := setRawDefaultsField(&d, k, probes[k]); !known || err != nil {
				t.Errorf("setRawDefaultsField(%q) known=%v err=%v — OMNI_%s and CLI "+
					"overrides would silently ignore it", k, known, err, strings.ToUpper(k))
			}
		}
		for k := range knownDefaultsPaths {
			if !contains(keys, k) {
				t.Errorf("knownDefaultsPaths names %q but rawDefaults has no such field", k)
			}
		}
	})

	t.Run("agent", func(t *testing.T) {
		keys := scalarKeys(t, reflect.TypeOf(rawAgent{}))
		for _, k := range keys {
			if !knownAgentPaths[k] {
				t.Errorf("rawAgent carries %q but knownAgentPaths omits it", k)
			}
			var a rawAgent
			if known, err := setRawAgentField(&a, k, probes[k]); !known || err != nil {
				t.Errorf("setRawAgentField(%q) known=%v err=%v", k, known, err)
			}
		}
		for k := range knownAgentPaths {
			if !contains(keys, k) {
				t.Errorf("knownAgentPaths names %q but rawAgent has no such field", k)
			}
		}
	})

	t.Run("every key has a probe", func(t *testing.T) {
		for _, typ := range []reflect.Type{reflect.TypeOf(rawDefaults{}), reflect.TypeOf(rawAgent{})} {
			for _, k := range scalarKeys(t, typ) {
				if _, ok := probes[k]; !ok {
					t.Errorf("no probe value for %q — add one when adding a key", k)
				}
			}
		}
	})
}

// TestSchemaRoundTrips drives each key through the textual path environment
// variables and CLI flags use, and checks the value arrives in Effective and
// is displayed. It also checks that setting one key moves *only* that key:
// an apply branch that assigns to a neighbouring field of the same type is a
// copy-paste error the compiler cannot catch, and every bool here has the
// same type.
func TestSchemaRoundTrips(t *testing.T) {
	loc := func(string) string { return "(probe)" }

	check := func(t *testing.T, shape, key string, e *Effective, issues []Issue) {
		t.Helper()
		for _, is := range issues {
			t.Fatalf("%s: unexpected issue setting %s: %s", shape, key, is)
		}
		var moved []Row
		for _, r := range e.Rows() {
			if r.Source == "(probe)" {
				moved = append(moved, r)
			}
		}
		if len(moved) == 0 {
			t.Fatalf("%s: setting %s changed nothing that `config show` reports", shape, key)
		}
		if len(moved) > 1 {
			t.Fatalf("%s: setting %s moved %d keys (%v) — an apply branch writes to the wrong field",
				shape, key, len(moved), moved)
		}
		if moved[0].Path != key {
			t.Fatalf("%s: setting %s moved %q instead", shape, key, moved[0].Path)
		}
		if !strings.Contains(moved[0].Value, probes[key]) {
			t.Errorf("%s: %s = %s, want a value derived from %q",
				shape, key, moved[0].Value, probes[key])
		}
	}

	for _, k := range scalarKeys(t, reflect.TypeOf(rawDefaults{})) {
		t.Run("defaults/"+k, func(t *testing.T) {
			var d rawDefaults
			if _, err := setRawDefaultsField(&d, k, probes[k]); err != nil {
				t.Fatalf("setRawDefaultsField: %v", err)
			}
			e := builtinDefaults("claude")
			var issues []Issue
			applyDefaults(e, d, loc, &issues)
			check(t, "defaults", k, e, issues)
		})
	}

	for _, k := range scalarKeys(t, reflect.TypeOf(rawAgent{})) {
		t.Run("agent/"+k, func(t *testing.T) {
			var a rawAgent
			if _, err := setRawAgentField(&a, k, probes[k]); err != nil {
				t.Fatalf("setRawAgentField: %v", err)
			}
			e := builtinDefaults("claude")
			var issues []Issue
			applyAgent(e, a, loc, &issues)
			check(t, "agent", k, e, issues)
		})
	}
}

// TestProjectScopeIsMinimal pins the ./.omni.conf allowlist. The project
// layer is untrusted input — a repo you cd into and did not write — so
// widening it is a security decision that must be made deliberately. If this
// fails, read loadProjectConfig and internal-docs/08-configuration.md
// §Security before changing the expectation.
func TestProjectScopeIsMinimal(t *testing.T) {
	for _, allowed := range []string{"mode", "route"} {
		if !projectAllowedPath(allowed) {
			t.Errorf("%q should be settable from ./.omni.conf", allowed)
		}
	}
	// Everything else the schema has must stay out.
	for _, k := range scalarKeys(t, reflect.TypeOf(rawAgent{})) {
		if k == "mode" {
			continue
		}
		if projectAllowedPath(k) {
			t.Errorf("%q is writable from ./.omni.conf — this is the untrusted-input boundary", k)
		}
	}
	// Named explicitly because §Security names them. record.enabled is here
	// because a repo that could switch recording on would be deciding to
	// write your prompts — its own source included — to disk for you.
	for _, forbidden := range []string{"binary", "upstream", "redact", "record.enabled"} {
		if projectAllowedPath(forbidden) {
			t.Errorf("%q is writable from ./.omni.conf", forbidden)
		}
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
