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
mode = "route"
`)
	e, err = LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("layer3: Load: %v", err)
	}
	if e.Mode.V != ModeRoute {
		t.Fatalf("layer3: mode = %q, want route", e.Mode.V)
	}
	// A different agent must not see the claude-scoped override.
	eCodex, err := LoadFrom(home, "codex")
	if err != nil {
		t.Fatalf("layer3 codex: Load: %v", err)
	}
	if eCodex.Mode.V != ModeOff {
		t.Fatalf("layer3: codex mode = %q, want off (unaffected by [agents.claude])", eCodex.Mode.V)
	}

	// Layer 4: agents/claude.conf drop-in beats the inline [agents.claude].
	writeTestFile(t, AgentConfigPath(home, "claude"), `
mode = "record"
`)
	e, err = LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("layer4: Load: %v", err)
	}
	if e.Mode.V != ModeRecord {
		t.Fatalf("layer4: mode = %q, want record (drop-in wins over inline)", e.Mode.V)
	}
	if !strings.Contains(e.Mode.Source, "agents/claude.conf") && !strings.Contains(e.Mode.Source, "claude.conf") {
		t.Fatalf("layer4: source = %q, want agents/claude.conf", e.Mode.Source)
	}

	// Layer 5: project config (./.omni.conf), CWD only.
	writeTestFile(t, ProjectConfigName, `
mode = "off"
`)
	e, err = LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("layer5: Load: %v", err)
	}
	if e.Mode.V != ModeOff {
		t.Fatalf("layer5: mode = %q, want off (project wins)", e.Mode.V)
	}
	if !strings.Contains(e.Mode.Source, ProjectConfigName) {
		t.Fatalf("layer5: source = %q, want %s", e.Mode.Source, ProjectConfigName)
	}

	// Layer 6: unscoped env var beats project config.
	t.Setenv("OMNI_MODE", "route")
	e, err = LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("layer6a: Load: %v", err)
	}
	if e.Mode.V != ModeRoute {
		t.Fatalf("layer6a: mode = %q, want route (env wins)", e.Mode.V)
	}
	if e.Mode.Source != "$OMNI_MODE" {
		t.Fatalf("layer6a: source = %q, want $OMNI_MODE", e.Mode.Source)
	}

	// Layer 6, more specific: agent-scoped env beats unscoped env.
	t.Setenv("OMNI_AGENTS__CLAUDE__MODE", "record")
	e, err = LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("layer6b: Load: %v", err)
	}
	if e.Mode.V != ModeRecord {
		t.Fatalf("layer6b: mode = %q, want record (agent-scoped env wins over unscoped)", e.Mode.V)
	}
	if e.Mode.Source != "$OMNI_AGENTS__CLAUDE__MODE" {
		t.Fatalf("layer6b: source = %q, want $OMNI_AGENTS__CLAUDE__MODE", e.Mode.Source)
	}
	// codex must not see claude's agent-scoped env var.
	eCodex, err = LoadFrom(home, "codex")
	if err != nil {
		t.Fatalf("layer6b codex: Load: %v", err)
	}
	if eCodex.Mode.V != ModeRoute {
		t.Fatalf("layer6b: codex mode = %q, want route (still sees unscoped OMNI_MODE only)", eCodex.Mode.V)
	}

	// Layer 7: CLI override beats everything.
	if err := e.Override(map[string]string{"mode": "off"}, "(cli flag)"); err != nil {
		t.Fatalf("layer7: Override: %v", err)
	}
	if e.Mode.V != ModeOff || e.Mode.Source != "(cli flag)" {
		t.Fatalf("layer7: mode = %+v, want off/(cli flag)", e.Mode)
	}
}

// TestDeepMergeIsPerKey checks that setting one sub-key of [record] at one
// layer does not clobber sibling keys set at other layers — i.e. merge is
// per-key, not whole-table replace.
func TestDeepMergeIsPerKey(t *testing.T) {
	home := testHome(t)
	testCWD(t)

	writeTestFile(t, GlobalConfigPath(home), `
[defaults.record]
redact = false
`)
	writeTestFile(t, AgentConfigPath(home, "claude"), `
[record]
bodies = false
`)

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if e.Record.Redact.V != false {
		t.Errorf("record.redact = %v, want false (from global defaults)", e.Record.Redact.V)
	}
	if !strings.Contains(e.Record.Redact.Source, "omni.conf") {
		t.Errorf("record.redact source = %q, want omni.conf", e.Record.Redact.Source)
	}
	if e.Record.Bodies.V != false {
		t.Errorf("record.bodies = %v, want false (from agent drop-in)", e.Record.Bodies.V)
	}
	if !strings.Contains(e.Record.Bodies.Source, "claude.conf") {
		t.Errorf("record.bodies source = %q, want claude.conf", e.Record.Bodies.Source)
	}
	// Untouched sibling keys still carry the built-in default.
	if e.Record.Enabled.V != true || e.Record.Enabled.Source != builtinSource {
		t.Errorf("record.enabled = %+v, want true/(built-in default) untouched", e.Record.Enabled)
	}
}

// TestRuleListReplacedAcrossLayers checks that a later layer's [[route]]
// list replaces the earlier one rather than merging into it. Rules are
// ordered and first-match-wins, so a merged list would need a defined
// cross-layer order — and there is none a reader could predict.
func TestRuleListReplacedAcrossLayers(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeTestFile(t, GlobalConfigPath(home), `
[[agents.claude.route]]
match = "claude-opus-5"
model = "claude-sonnet-5"
`)
	writeTestFile(t, AgentConfigPath(home, "claude"), `
[[route]]
match = "claude-haiku-5"
model = "claude-sonnet-5"
`)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(e.Routes.V) != 1 {
		t.Fatalf("got %d rules, want only the drop-in's: %+v", len(e.Routes.V), e.Routes.V)
	}
	if e.Routes.V[0].Match != "claude-haiku-5" {
		t.Errorf("match = %q, want the drop-in's rule to win", e.Routes.V[0].Match)
	}
}
