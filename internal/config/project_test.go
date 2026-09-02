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
// must be restricted to mode and route — and must NOT be able to set
// binary, upstream, or redact. `binary` in particular would be arbitrary
// code execution on cd if this allowlist had a hole in it.
func TestProjectConfigAllowlist(t *testing.T) {
	t.Run("allowed keys apply", func(t *testing.T) {
		home := testHome(t)
		testCWD(t)
		writeTestFile(t, ProjectConfigName, `
mode = "off"

[[route]]
match = "claude-opus-5"
model = "claude-sonnet-5"
`)
		e, err := LoadFrom(home, "claude")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if e.Mode.V != ModeOff {
			t.Errorf("mode = %q, want off", e.Mode.V)
		}
		if !strings.Contains(e.Mode.Source, ProjectConfigName) {
			t.Errorf("mode source = %q, want %s", e.Mode.Source, ProjectConfigName)
		}
		if len(e.Routes.V) != 1 || e.Routes.V[0].Model != "claude-sonnet-5" {
			t.Errorf("routes = %+v, want the rename honored", e.Routes.V)
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
				// Redaction is the one remaining [defaults] key a hostile
				// repo would most want to switch off: it would put the
				// agent's own credentials into the session transcript.
				name: "redact",
				toml: `redact = false`,
				checkNot: func(e *Effective) (string, bool) {
					return "redact", e.Redact.V == false
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
