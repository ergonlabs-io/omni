// Command omni runs coding agents through an interception proxy.
//
// See internal-docs/ for design. Phase 0 scope: recording only.
package main

import (
	"fmt"
	"os"
)

// Build metadata, injected via -ldflags -X. A version string without a commit
// is useless for bug reports. See internal-docs/09-cli-design.md §7.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	inv, err := ParseArgs(argv)
	if err != nil {
		errorf("%v", err)
		return exitUsage
	}

	switch {
	case inv.WantVersion:
		// stdout: this is requested output, not a diagnostic.
		fmt.Fprintln(os.Stdout, versionString())
		return exitOK
	case inv.WantHelp:
		fmt.Fprint(os.Stdout, helpText())
		return exitOK
	case inv.Subcommand != "":
		return runSubcommand(inv)
	case inv.Agent != "":
		return runAgent(inv)
	default:
		fmt.Fprint(os.Stderr, helpText())
		return exitUsage
	}
}

// errorf writes an omni-originated error to stderr.
//
// Every omni-originated failure carries the "omni: " prefix; the child's output
// never does. That prefix is what disambiguates our exit codes from the
// child's. See internal-docs/09-cli-design.md §4.
func errorf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "omni: "+format+"\n", a...)
}
