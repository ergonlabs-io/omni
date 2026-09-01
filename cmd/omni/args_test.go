package main

import (
	"reflect"
	"testing"
)

// The passthrough rule is the one thing that must never change.
// See internal-docs/09-cli-design.md §1.
func TestPassthroughRule(t *testing.T) {
	tests := []struct {
		name      string
		argv      []string
		agent     string
		agentArgs []string
		subcmd    string
	}{
		{
			name:  "bare agent",
			argv:  []string{"claude"},
			agent: "claude",
		},
		{
			// The canonical ambiguity: --model belongs to claude, not omni.
			name:      "agent flags are never omni's",
			argv:      []string{"claude", "--model", "opus"},
			agent:     "claude",
			agentArgs: []string{"--model", "opus"},
		},
		{
			// omni claude --help must show CLAUDE's help, not omni's.
			name:      "help after agent goes to agent",
			argv:      []string{"claude", "--help"},
			agent:     "claude",
			agentArgs: []string{"--help"},
		},
		{
			name:      "omni flags before agent",
			argv:      []string{"--mode", "route", "claude"},
			agent:     "claude",
			agentArgs: nil,
		},
		{
			name:      "omni flags before, agent flags after",
			argv:      []string{"--mode", "route", "claude", "--resume", "x"},
			agent:     "claude",
			agentArgs: []string{"--resume", "x"},
		},
		{
			name:      "equals form",
			argv:      []string{"--mode=route", "claude", "-p", "hi"},
			agent:     "claude",
			agentArgs: []string{"-p", "hi"},
		},
		{
			name:      "explicit terminator",
			argv:      []string{"--dry-run", "--", "claude", "--mode", "x"},
			agent:     "claude",
			agentArgs: []string{"--mode", "x"},
		},
		{
			// A flag omni owns, appearing after the agent, is the AGENT's.
			name:      "omni flag name after agent belongs to agent",
			argv:      []string{"claude", "--dry-run"},
			agent:     "claude",
			agentArgs: []string{"--dry-run"},
		},
		{
			name:   "reserved subcommand",
			argv:   []string{"config", "show"},
			subcmd: "config",
		},
		{
			name:      "run disambiguates",
			argv:      []string{"run", "claude", "--help"},
			agent:     "claude",
			agentArgs: []string{"--help"},
		},
		{
			name:      "agent args preserved verbatim including empties",
			argv:      []string{"claude", "-p", "", "--x"},
			agent:     "claude",
			agentArgs: []string{"-p", "", "--x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv, err := ParseArgs(tt.argv)
			if err != nil {
				t.Fatalf("ParseArgs(%v) error: %v", tt.argv, err)
			}
			if inv.Agent != tt.agent {
				t.Errorf("Agent = %q, want %q", inv.Agent, tt.agent)
			}
			if inv.Subcommand != tt.subcmd {
				t.Errorf("Subcommand = %q, want %q", inv.Subcommand, tt.subcmd)
			}
			if !reflect.DeepEqual(inv.AgentArgs, tt.agentArgs) {
				t.Errorf("AgentArgs = %#v, want %#v", inv.AgentArgs, tt.agentArgs)
			}
		})
	}
}

func TestOmniLevelHelpAndVersion(t *testing.T) {
	inv, err := ParseArgs([]string{"--help"})
	if err != nil || !inv.WantHelp {
		t.Errorf("--help should set WantHelp; got %+v err=%v", inv, err)
	}
	inv, err = ParseArgs([]string{"--version"})
	if err != nil || !inv.WantVersion {
		t.Errorf("--version should set WantVersion; got %+v err=%v", inv, err)
	}
	// After the agent name it is NOT omni's.
	inv, err = ParseArgs([]string{"claude", "--help"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.WantHelp {
		t.Error("--help after agent name must not be treated as omni's")
	}
}

func TestRepeatableFlag(t *testing.T) {
	inv, err := ParseArgs([]string{
		"--model-map", "a=b", "--model-map", "c=d", "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := inv.All("--model-map")
	want := []string{"a=b", "c=d"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("All(--model-map) = %v, want %v", got, want)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		argv []string
	}{
		{"unknown flag", []string{"--nope", "claude"}},
		{"missing value", []string{"--mode"}},
		{"value on bool flag", []string{"--dry-run=yes", "claude"}},
		{"run without agent", []string{"run"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseArgs(tt.argv); err == nil {
				t.Errorf("ParseArgs(%v) = nil error, want error", tt.argv)
			}
		})
	}
}

func TestEmptyInvocation(t *testing.T) {
	inv, err := ParseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Agent != "" || inv.Subcommand != "" {
		t.Errorf("empty argv should yield no agent or subcommand, got %+v", inv)
	}
}
