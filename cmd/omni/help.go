package main

import (
	"fmt"
	"strings"

	"github.com/ergonlabs-io/omni/internal/profile"
)

// helpText renders `omni --help`, following internal-docs/09-cli-design.md §5.
//
// Examples come before flags because people scan for the example matching their
// intent. The AGENTS section shows detection status because "is my agent
// installed" is a real question and --help is where people look. The
// passthrough rule is restated at the bottom because it is the single most
// surprising behavior.
func helpText() string {
	var b strings.Builder

	b.WriteString(`omni — run coding agents through an interception proxy

USAGE
    omni [flags] <agent> [agent-args...]
    omni <subcommand> [flags]

EXAMPLES
    omni claude                                  record a Claude Code session
    omni --mode route claude                     apply configured model routing
    omni --model-map claude-opus-5=claude-sonnet-5 claude
    omni codex --search                          flags after the agent go to it
    omni config show --agent claude              effective config + provenance

AGENTS
`)

	for _, p := range profile.All() {
		status := "not found on PATH"
		if path, err := p.Resolve(); err == nil {
			status = "detected: " + path
		}
		fmt.Fprintf(&b, "    %-11s %-22s (%s)\n", p.Name, p.Desc, status)
	}

	b.WriteString(`
SUBCOMMANDS
    init         create or repair ~/.omni
    config       show, check, and locate configuration
    run          run an agent (unambiguous form)

  Reserved, not yet implemented — these exit with an error:
    ca           manage the Tier 2 certificate authority
    sessions     list and prune recorded sessions
    completions  generate shell completions

FLAGS
    --mode <off|record>         interception mode (default: from config)
    --record-only               shorthand for --mode record
    --model-map <from=to>       one-off model rewrite; repeatable
    --dry-run                   print what would happen; launch nothing
    -v, --verbose               diagnostics to stderr; repeatable
    --version                   version, commit, build date

  Reserved, not yet implemented:
    --all-traffic               Tier 2 full MITM; errors on agents that
                                cannot support it, otherwise does nothing

Everything after <agent> is passed through untouched. ` + "`omni claude --help`" + `
shows Claude Code's help; use ` + "`omni --help`" + ` for this.
`)

	return b.String()
}
