package main

import (
	"fmt"
	"strings"

	"github.com/ergonlabs-io/omni/internal/profile"
)

// Argument parsing.
//
// The load-bearing rule, from internal-docs/09-cli-design.md §1:
//
//	omni [omni-flags] <agent> [agent-args...]
//	                          ^^^^^^^^^^^^^^ never parsed by omni
//
// Everything after the agent name belongs to the agent, verbatim. omni's own
// flags come before it. `--` is also accepted as an explicit terminator.
//
// The correct consequence is that `omni claude --help` prints Claude Code's
// help, not omni's. This rule must never change: users build muscle memory and
// scripts on it, and any cleverness here produces a tool that silently swallows
// the agent's arguments.

// flagSpec declares whether an omni global flag consumes a following value.
// We need this to know where the agent name starts: in `omni --mode route
// claude`, "route" is a flag value, not the agent.
var flagSpec = map[string]bool{ // name -> takes a value
	"--mode":        true,
	"--model-map":   true,
	"--all-traffic": false,
	"--dry-run":     false,
	"--verbose":     false,
	"-v":            false,
	"--help":        false,
	"-h":            false,
	"--version":     false,
	"--record-only": false,
}

// Invocation is the parsed command line.
type Invocation struct {
	// Flags holds omni's own global flags, in order encountered.
	Flags []Flag
	// Subcommand is set when the first non-flag token is a reserved name.
	Subcommand string
	// SubcommandArgs are the remaining tokens for a subcommand.
	SubcommandArgs []string
	// Agent is the agent name, when not a subcommand.
	Agent string
	// AgentArgs are passed to the agent verbatim, never interpreted.
	AgentArgs []string
	// WantHelp / WantVersion are set by omni-level --help / --version, which
	// are only omni's when they appear before the agent name.
	WantHelp    bool
	WantVersion bool
}

// Flag is one parsed omni global flag.
type Flag struct {
	Name  string
	Value string
}

// Get returns the last value for a flag name and whether it was present.
func (inv *Invocation) Get(name string) (string, bool) {
	val, ok := "", false
	for _, f := range inv.Flags {
		if f.Name == name {
			val, ok = f.Value, true
		}
	}
	return val, ok
}

// Has reports whether a boolean flag was set.
func (inv *Invocation) Has(name string) bool {
	_, ok := inv.Get(name)
	return ok
}

// All returns every value given for a repeatable flag, in order.
func (inv *Invocation) All(name string) []string {
	var out []string
	for _, f := range inv.Flags {
		if f.Name == name {
			out = append(out, f.Value)
		}
	}
	return out
}

// ParseArgs splits argv (excluding argv[0]) per the passthrough rule.
func ParseArgs(argv []string) (*Invocation, error) {
	inv := &Invocation{}

	i := 0
	for ; i < len(argv); i++ {
		arg := argv[i]

		// Explicit terminator: everything after is the agent invocation.
		if arg == "--" {
			i++
			break
		}

		// First non-flag token ends omni's flag section.
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			break
		}

		// --name=value form.
		if eq := strings.IndexByte(arg, '='); eq > 0 {
			name, value := arg[:eq], arg[eq+1:]
			takesValue, known := flagSpec[name]
			if !known {
				return nil, unknownFlag(name)
			}
			if !takesValue {
				return nil, fmt.Errorf("flag %s does not take a value", name)
			}
			inv.Flags = append(inv.Flags, Flag{name, value})
			continue
		}

		takesValue, known := flagSpec[arg]
		if !known {
			return nil, unknownFlag(arg)
		}
		if takesValue {
			if i+1 >= len(argv) {
				return nil, fmt.Errorf("flag %s requires a value", arg)
			}
			i++
			inv.Flags = append(inv.Flags, Flag{arg, argv[i]})
			continue
		}
		inv.Flags = append(inv.Flags, Flag{arg, ""})
	}

	// omni-level --help / --version only count before the agent name.
	inv.WantHelp = inv.Has("--help") || inv.Has("-h")
	inv.WantVersion = inv.Has("--version")

	rest := argv[i:]
	if len(rest) == 0 {
		return inv, nil
	}

	name := rest[0]

	// Reserved names always resolve as subcommands and can never be agents.
	// `omni run <agent>` is the always-unambiguous escape hatch.
	if profile.IsReserved(name) {
		if name == "run" {
			if len(rest) < 2 {
				return nil, fmt.Errorf("run requires an agent name")
			}
			inv.Agent = rest[1]
			inv.AgentArgs = trim(rest[2:])
			return inv, nil
		}
		inv.Subcommand = name
		inv.SubcommandArgs = trim(rest[1:])
		return inv, nil
	}

	inv.Agent = name
	inv.AgentArgs = trim(rest[1:]) // verbatim, never interpreted
	return inv, nil
}

// trim normalizes an empty slice to nil so callers need not distinguish
// "no args" from "an empty arg list".
func trim(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func unknownFlag(name string) error {
	return fmt.Errorf("unknown flag %s\n\n"+
		"  omni's own flags must come before the agent name.\n"+
		"  everything after the agent name is passed to it verbatim:\n"+
		"    omni --mode route claude --some-claude-flag", name)
}
