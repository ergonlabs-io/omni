package config

import (
	"strings"
	"testing"
)

// TestPrecedenceOrdering builds up config one layer at a time and checks
// that each new, higher layer wins for `mode`, while lower layers remain
// visible in provenance until overridden. This is the core contract from
// internal-docs/08-configuration.md §Precedence.
func TestPrecedenceOrdering(t *testing.T) {
	home := testHome(t)
	testCWD(t)

	// Layer 1: built-in default only.
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("layer1: Load: %v", err)
	}
	if e.Mode.V != ModeRecord || e.Mode.Source != builtinSource {
		t.Fatalf("layer1: mode = %+v, want record/(built-in default)", e.Mode)
	}
	if e.RecordEnabled.V || e.RecordEnabled.Source != builtinSource {
		t.Fatalf("layer1: record.enabled = %+v, want false/(built-in default)", e.RecordEnabled)
	}

	// Layer 2: global [defaults].
	writeTestFile(t, GlobalConfigPath(home), `
[defaults]
mode = "off"
`)
	e, err = LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("layer2: Load: %v", err)
	}
	if e.Mode.V != ModeOff {
		t.Fatalf("layer2: mode = %q, want off", e.Mode.V)
	}
	if !strings.Contains(e.Mode.Source, "omni.conf") {
		t.Fatalf("layer2: source = %q, want omni.conf", e.Mode.Source)
	}

	// Layer 3: global [agents.claude] inline override.
	writeTestFile(t, GlobalConfigPath(home), `
[defaults]
mode = "off"

[agents.claude]
mode = "record"
`)
	e, err = LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("layer3: Load: %v", err)
	}
	if e.Mode.V != ModeRecord {
		t.Fatalf("layer3: mode = %q, want record", e.Mode.V)
	}
	// A different agent must not see the claude-scoped override.
	eCodex, err := LoadFrom(home, "codex")
	if err != nil {
		t.Fatalf("layer3 codex: Load: %v", err)
	}
	if eCodex.Mode.V != ModeOff {
		t.Fatalf("layer3: codex mode = %q, want off (unaffected by [agents.claude])", eCodex.Mode.V)
	}

	// Layer 4: project config (./.omni.conf), CWD only.
	writeTestFile(t, ProjectConfigName, `
mode = "off"
`)
	e, err = LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("layer4: Load: %v", err)
	}
	if e.Mode.V != ModeOff {
		t.Fatalf("layer4: mode = %q, want off (project wins)", e.Mode.V)
	}
	if !strings.Contains(e.Mode.Source, ProjectConfigName) {
		t.Fatalf("layer4: source = %q, want %s", e.Mode.Source, ProjectConfigName)
	}

	// Layer 5: unscoped env var beats project config.
	t.Setenv("OMNI_MODE", "record")
	e, err = LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("layer5a: Load: %v", err)
	}
	if e.Mode.V != ModeRecord {
		t.Fatalf("layer5a: mode = %q, want record (env wins)", e.Mode.V)
	}
	if e.Mode.Source != "$OMNI_MODE" {
		t.Fatalf("layer5a: source = %q, want $OMNI_MODE", e.Mode.Source)
	}

	// Layer 5, more specific: agent-scoped env beats unscoped env.
	t.Setenv("OMNI_AGENTS__CLAUDE__MODE", "off")
	e, err = LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("layer5b: Load: %v", err)
	}
	if e.Mode.V != ModeOff {
		t.Fatalf("layer5b: mode = %q, want off (agent-scoped env wins over unscoped)", e.Mode.V)
	}
	if e.Mode.Source != "$OMNI_AGENTS__CLAUDE__MODE" {
		t.Fatalf("layer5b: source = %q, want $OMNI_AGENTS__CLAUDE__MODE", e.Mode.Source)
	}
	// codex must not see claude's agent-scoped env var.
	eCodex, err = LoadFrom(home, "codex")
	if err != nil {
		t.Fatalf("layer5b codex: Load: %v", err)
	}
	if eCodex.Mode.V != ModeRecord {
		t.Fatalf("layer5b: codex mode = %q, want record (still sees unscoped OMNI_MODE only)", eCodex.Mode.V)
	}

	// Layer 6: CLI override beats everything.
	if err := e.Override(map[string]string{"mode": "record"}, "(cli flag)"); err != nil {
		t.Fatalf("layer6: Override: %v", err)
	}
	if e.Mode.V != ModeRecord || e.Mode.Source != "(cli flag)" {
		t.Fatalf("layer6: mode = %+v, want record/(cli flag)", e.Mode)
	}
}

// TestDeepMergeIsPerKey checks that a higher layer setting one key does not
// clobber the keys it did not mention — merge is per-key, not whole-section
// replace.
func TestDeepMergeIsPerKey(t *testing.T) {
	home := testHome(t)
	testCWD(t)

	writeTestFile(t, GlobalConfigPath(home), `
[defaults]
mode   = "off"
redact = false

[agents.claude]
mode = "record"
`)

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// [agents.claude] set only mode...
	if e.Mode.V != ModeRecord {
		t.Errorf("mode = %v, want record (from [agents.claude])", e.Mode.V)
	}
	// ...so redact must survive from [defaults], not revert to its built-in.
	if e.Redact.V != false {
		t.Errorf("redact = %v, want false (from [defaults], untouched by the agent section)", e.Redact.V)
	}
	if e.Redact.Source == builtinSource {
		t.Errorf("redact source = %q, want the [defaults] line; the agent section replaced a key it never set",
			e.Redact.Source)
	}
	// The two keys came from different lines, and provenance must say so.
	if e.Mode.Source == e.Redact.Source {
		t.Errorf("mode and redact both report %q; per-key provenance lost", e.Mode.Source)
	}
}

// TestRuleListReplacedAcrossLayers checks that a later layer's [[route]]
// list replaces the earlier one rather than merging into it. Rules are
// ordered and first-match-wins, so a merged list would need a defined
// cross-layer order — and there is none a reader could predict.
func TestRuleListReplacedAcrossLayers(t *testing.T) {
	home := testHome(t)
	cwd := testCWD(t)
	writeTestFile(t, GlobalConfigPath(home), `
[[agents.claude.route]]
match = "claude-opus-5"
model = "claude-sonnet-5"
`)
	// The project layer sits above the global one, so its list replaces
	// rather than appends: rules are ordered and first-match-wins, and
	// there is no cross-layer order a reader could predict.
	writeTestFile(t, ProjectConfigPath(cwd), `
[[route]]
match = "claude-haiku-5"
model = "claude-sonnet-5"
`)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(e.Routes.V) != 1 {
		t.Fatalf("got %d rules, want only the project config's: %+v", len(e.Routes.V), e.Routes.V)
	}
	if e.Routes.V[0].Match != "claude-haiku-5" {
		t.Errorf("match = %q, want the project config's rule to win", e.Routes.V[0].Match)
	}
}
