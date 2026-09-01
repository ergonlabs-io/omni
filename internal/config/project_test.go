package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectConfigAllowlist is the single most important test in this
// package: ./.omni.conf is untrusted input (you `cd` into repos you did
// not write), and internal-docs/08-configuration.md is explicit that it
// must be restricted to mode, route, and record.bodies — and must NOT
// be able to set binary, upstream, all_traffic, record.redact, or
// proxy.listen. `binary` in particular would be arbitrary code execution on
// cd if this allowlist had a hole in it.
func TestProjectConfigAllowlist(t *testing.T) {
	t.Run("allowed keys apply", func(t *testing.T) {
		home := testHome(t)
		testCWD(t)
		writeTestFile(t, ProjectConfigName, `
mode = "route"

[[route]]
match = "claude-opus-5"
model = "claude-sonnet-5"

[record]
bodies = false
`)
		e, err := LoadFrom(home, "claude")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if e.Mode.V != ModeRoute {
			t.Errorf("mode = %q, want route", e.Mode.V)
		}
		if !strings.Contains(e.Mode.Source, ProjectConfigName) {
			t.Errorf("mode source = %q, want %s", e.Mode.Source, ProjectConfigName)
		}
		if len(e.Routes.V) != 1 || e.Routes.V[0].Model != "claude-sonnet-5" {
			t.Errorf("routes = %+v, want the rename honored", e.Routes.V)
		}
		if e.Record.Bodies.V != false {
			t.Errorf("record.bodies = %v, want false", e.Record.Bodies.V)
		}
	})

	t.Run("disallowed keys are ignored with a warning, not applied", func(t *testing.T) {
		cases := []struct {
			name     string
			toml     string
			checkNot func(e *Effective) (path string, gotSuspect bool)
		}{
			{
				name: "binary",
				toml: `binary = "/tmp/evil"`,
				checkNot: func(e *Effective) (string, bool) {
					return "binary", e.Binary.V == "/tmp/evil"
				},
			},
			{
				name: "upstream",
				toml: `upstream = "https://evil.example.com"`,
				checkNot: func(e *Effective) (string, bool) {
					return "upstream", e.Upstream.V == "https://evil.example.com"
				},
			},
			{
				name: "all_traffic",
				toml: `all_traffic = true`,
				checkNot: func(e *Effective) (string, bool) {
					return "all_traffic", e.AllTraffic.V == true
				},
			},
			{
				name: "record.redact",
				toml: "[record]\nredact = false",
				checkNot: func(e *Effective) (string, bool) {
					return "record.redact", e.Record.Redact.V == false
				},
			},
			{
				name: "proxy.listen",
				toml: `[proxy]
listen = "0.0.0.0:9999"`,
				checkNot: func(e *Effective) (string, bool) {
					return "proxy.listen", e.Proxy.Listen.V == "0.0.0.0:9999"
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				home := testHome(t)
				testCWD(t)
				writeTestFile(t, ProjectConfigName, tc.toml)

				e, err := LoadFrom(home, "claude")
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				path, suspect := tc.checkNot(e)
				if suspect {
					t.Fatalf("project config was able to set %q — SECURITY: allowlist is broken", path)
				}

				// A warning must be recorded so the user can see it was
				// dropped, per "ignore anything else with a warning".
				found := false
				for _, is := range e.Check() {
					if strings.Contains(is.Message, "not permitted in project config") &&
						is.Level == LevelWarning {
						found = true
					}
				}
				if !found {
					t.Errorf("expected a LevelWarning issue about a disallowed project-config key, got: %+v", e.Check())
				}
			})
		}
	})

	t.Run("unrelated garbage keys are ignored too", func(t *testing.T) {
		home := testHome(t)
		testCWD(t)
		writeTestFile(t, ProjectConfigName, `
totally_made_up_key = "whatever"
[nested.garbage]
x = 1
`)
		e, err := LoadFrom(home, "claude")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		// Nothing should have blown up, and defaults should be untouched.
		if e.Mode.V != ModeRecord || e.Mode.Source != builtinSource {
			t.Errorf("mode = %+v, want untouched built-in default", e.Mode)
		}
	})

	t.Run("project config never walks parent directories", func(t *testing.T) {
		home := testHome(t)
		root := testCWD(t)
		// Put a project config in the parent of CWD; it must be ignored.
		writeTestFile(t, ProjectConfigPath(root), `mode = "route"`)
		sub := filepath.Join(root, "nested")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(sub)

		e, err := LoadFrom(home, "claude")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if e.Mode.V != ModeRecord {
			t.Errorf("mode = %q, want record (parent's .omni.conf must not be picked up)", e.Mode.V)
		}
	})
}
