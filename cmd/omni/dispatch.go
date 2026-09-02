package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ergonlabs-io/omni/internal/config"
	"github.com/ergonlabs-io/omni/internal/profile"
)

// runAgent resolves and launches an agent.
//
// Phase 0 wiring is pending: config loading, proxy startup, and the PTY
// launcher land as those packages complete. What is implemented here is the
// validation that must happen BEFORE the child launches — failing after the
// agent has started is much worse than failing in 5ms
// (internal-docs/09-cli-design.md §9).
func runAgent(inv *Invocation) int {
	p := profile.Lookup(inv.Agent)
	if p == nil {
		errorf("agent %q not found\n", inv.Agent)
		if s := profile.Suggest(inv.Agent); len(s) > 0 {
			fmt.Fprintf(os.Stderr, "  did you mean: %s\n\n", strings.Join(s, ", "))
		}
		fmt.Fprintf(os.Stderr, "  known agents: %s\n", strings.Join(profile.Names(), ", "))
		fmt.Fprintf(os.Stderr, "  add your own: ~/.omni/profiles.d/<name>.conf\n")
		return exitUsage
	}

	if _, err := p.Resolve(); err != nil {
		errorf("%v\n", err)
		fmt.Fprintf(os.Stderr, "  install %s, or set `binary` under [agents.%s] in ~/.omni/omni.conf\n",
			p.Binary, p.Name)
		return exitUnavailable
	}

	// --all-traffic must fail loudly where Tier 2 is unsupported rather than
	// silently leaving traffic unintercepted (internal-docs/03).
	if inv.Has("--all-traffic") && !p.SupportsTier2() {
		errorf("agent %q does not support --all-traffic\n", p.Name)
		fmt.Fprintf(os.Stderr,
			"  no confirmed CA trust mechanism for this agent's runtime.\n"+
				"  its LLM traffic is still intercepted without --all-traffic.\n")
		return exitUsage
	}

	// --model-map requires a wire format we can decode.
	if len(inv.All("--model-map")) > 0 && !p.APIStyle.CanRewrite() {
		errorf("cannot apply --model-map for agent %q\n", p.Name)
		fmt.Fprintf(os.Stderr,
			"  model rewriting is Anthropic-only in this version.\n"+
				"  %s sessions are recorded but not rewritten.\n", p.Name)
		return exitUsage
	}

	binPath, _ := p.Resolve()

	eff, err := config.Load(p.Name)
	if err != nil {
		errorf("%v", err)
		return exitConfig
	}
	if eff.Binary.V != "" {
		binPath = eff.Binary.V
	}

	// Apply CLI flags as the highest precedence layer.
	overrides := map[string]string{}
	if v, ok := inv.Get("--mode"); ok {
		overrides["mode"] = v
	}
	if inv.Has("--record-only") {
		overrides["mode"] = string(config.ModeRecord)
	}
	// --all-traffic sets nothing: Tier 2 full MITM is not implemented, so
	// there is no config key for it to move. The flag survives only for the
	// SupportsTier2 rejection above, which is a real check.
	if len(overrides) > 0 {
		if err := eff.Override(overrides, "command line"); err != nil {
			errorf("%v", err)
			return exitUsage
		}
	}

	// Config problems must surface before the child launches.
	if issues := eff.Check(); len(issues) > 0 {
		for _, is := range issues {
			fmt.Fprintf(os.Stderr, "omni: %s: %s (%s)\n", is.Path, is.Message, is.Source)
		}
		if eff.HasErrors() {
			return exitConfig
		}
	}

	// --model-map is layer 6 for routing: each flag becomes a rule ahead of
	// the file-configured ones, so a one-off rewrite wins over config
	// without the user having to edit anything.
	if err := applyModelMapFlags(eff, inv.All("--model-map")); err != nil {
		errorf("%v", err)
		return exitUsage
	}

	if inv.Has("--dry-run") {
		return dryRun(inv, p, eff, binPath)
	}
	return launch(inv, p, eff, binPath)
}

func runSubcommand(inv *Invocation) int {
	switch inv.Subcommand {
	case "help":
		fmt.Fprint(os.Stdout, helpText())
		return exitOK
	case "version":
		fmt.Fprintln(os.Stdout, versionString())
		return exitOK
	case "init":
		return runInit()
	case "config":
		return runConfig(inv)
	case "ca", "sessions", "completions":
		errorf("not yet implemented: %s", inv.Subcommand)
		return exitSoftware
	default:
		errorf("unknown subcommand %q", inv.Subcommand)
		return exitUsage
	}
}

func runInit() int {
	home, err := config.Home()
	if err != nil {
		errorf("%v", err)
		return exitConfig
	}
	created, err := config.Init(home)
	if err != nil {
		errorf("%v", err)
		return exitConfig
	}
	if len(created) == 0 {
		fmt.Fprintf(os.Stdout, "%s already initialized; nothing to do\n", home)
	}
	for _, path := range created {
		fmt.Fprintf(os.Stdout, "created %s\n", path)
	}
	// Init does not tighten a directory that already existed, so say so
	// rather than leave a loose home looking like a successful bootstrap.
	warnPermissions(home)
	return exitOK
}

// warnPermissions prints any permission problems in the omni home. Warnings
// only: they never change the exit code, and never stop a launch.
func warnPermissions(home string) {
	for _, w := range config.PermissionWarnings(home) {
		fmt.Fprintf(os.Stderr, "omni: warning: %s\n", w)
	}
}

func runConfig(inv *Invocation) int {
	args := inv.SubcommandArgs
	if len(args) == 0 {
		errorf("config requires a subcommand: show, check, path")
		return exitUsage
	}

	// `--agent <name>` selects which agent's effective config to resolve.
	agent := ""
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--agent" {
			agent = args[i+1]
		}
	}

	switch args[0] {
	case "path":
		home, err := config.Home()
		if err != nil {
			errorf("%v", err)
			return exitConfig
		}
		fmt.Fprintln(os.Stdout, home)
		return exitOK

	case "show":
		eff, err := config.Load(agent)
		if err != nil {
			errorf("%v", err)
			return exitConfig
		}
		fmt.Fprint(os.Stdout, eff.Show())
		return exitOK

	case "check":
		eff, err := config.Load(agent)
		if err != nil {
			errorf("%v", err)
			return exitConfig
		}
		if home, herr := config.Home(); herr == nil {
			warnPermissions(home)
		}
		issues := eff.Check()
		if len(issues) == 0 {
			fmt.Fprintln(os.Stdout, "config ok")
			return exitOK
		}
		for _, is := range issues {
			fmt.Fprintf(os.Stderr, "omni: %s: %s (%s)\n", is.Path, is.Message, is.Source)
		}
		if eff.HasErrors() {
			return exitConfig
		}
		return exitOK

	default:
		errorf("unknown config subcommand %q (want: show, check, path)", args[0])
		return exitUsage
	}
}
