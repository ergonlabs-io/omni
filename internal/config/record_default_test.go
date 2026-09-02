package config

import "testing"

// TestRecordingIsOffByDefault pins the decision that recording is opt-in.
// A recorded session holds the prompts an agent sent, which routinely carry
// source code and secrets from the working directory; switching that on for
// someone silently is not a default we want to drift back into.
//
// It is deliberately separate from the Mode default, which stays "record":
// the two were one switch once, and re-merging them would either disable
// routing for everyone or re-enable recording for everyone.
func TestRecordingIsOffByDefault(t *testing.T) {
	e := builtinDefaults("claude")
	if e.RecordEnabled.V {
		t.Errorf("record.enabled defaults to true — recording must be opt-in")
	}
	if e.Mode.V == ModeOff {
		t.Errorf("mode defaults to off — routing rules would be silently inert")
	}
}

// TestRecordingCanBeEnabledPerAgent checks the key merges like every other
// agent-scoped setting, so `omni --record claude` and an [agents.claude]
// table reach the same place.
func TestRecordingCanBeEnabledPerAgent(t *testing.T) {
	e := builtinDefaults("claude")
	var a rawAgent
	if known, err := setRawAgentField(&a, "record.enabled", "true"); !known || err != nil {
		t.Fatalf("setRawAgentField(record.enabled): known=%v err=%v", known, err)
	}
	var issues []Issue
	applyAgent(e, a, func(string) string { return "(test)" }, &issues)
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if !e.RecordEnabled.V || e.RecordEnabled.Source != "(test)" {
		t.Errorf("record.enabled = %+v, want true/(test)", e.RecordEnabled)
	}
}
