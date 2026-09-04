package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ergonlabs-io/omni/internal/config"
	"github.com/ergonlabs-io/omni/internal/launcher"
	"github.com/ergonlabs-io/omni/internal/profile"
	"github.com/ergonlabs-io/omni/internal/proxy"
	"github.com/ergonlabs-io/omni/internal/record"
)

// launch starts the proxy, then runs the agent in a PTY with its API traffic
// redirected through it. Everything that can fail is validated by the caller
// before we get here — failing after the child has started is much worse than
// failing in 5ms (internal-docs/09-cli-design.md §9).
func launch(inv *Invocation, p *profile.Profile, eff *config.Effective, binPath string) int {
	verbose := inv.Has("--verbose") || inv.Has("-v")

	upstream := p.Upstream
	if eff.Upstream.V != "" {
		upstream = eff.Upstream.V
	}
	up, err := url.Parse(upstream)
	if err != nil {
		errorf("invalid upstream %q: %v", upstream, err)
		return exitConfig
	}

	// Recorder. Recording is opt-in (`--record`, or record.enabled in
	// config) and additionally requires a mode that tees traffic at all.
	//
	// Failing to record must never become the user's outage
	// (internal-docs/05-constraints.md §5), so a recorder that cannot be
	// created downgrades to no recording rather than aborting the session.
	var rec *record.Recorder
	if eff.Mode.V != config.ModeOff && eff.RecordEnabled.V {
		home, herr := config.Home()
		if herr != nil {
			errorf("warning: cannot resolve omni home, recording disabled: %v", herr)
		} else {
			rec, err = record.New(
				filepath.Join(home, "sessions"), p.Name, version,
				record.WithRedaction(eff.Redact.V),
			)
			if err != nil {
				errorf("warning: recording disabled: %v", err)
				rec = nil
			}
		}
	}
	if rec != nil {
		defer func() {
			if cerr := rec.Close(); cerr != nil {
				errorf("warning: closing session record: %v", cerr)
			}
		}()
	}

	// Routing is resolved before the proxy binds, so a bad backend or a
	// missing credential fails in milliseconds rather than after the child
	// is on screen (internal-docs/09-cli-design.md §9).
	router, err := resolveRouter(eff, p)
	if err != nil {
		errorf("%v", err)
		return exitConfig
	}
	if router != nil && verbose {
		router.OnRoute = func(from, to, backend string) {
			if backend == "" {
				errorf("route: %s -> %s", from, to)
				return
			}
			errorf("route: %s -> %s via %s", from, to, backend)
		}
		// Rendering lives here rather than in the proxy, on the same split
		// as OnRoute: the proxy reports the fact, cmd/omni decides how it
		// reads. The agent will print its own retry counter a moment later
		// and say nothing about where the request went — this line is the
		// only thing that names the backend.
		router.OnRouteFailure = func(f proxy.RouteFailure) {
			where := "the default upstream"
			if f.Backend != "" {
				where = fmt.Sprintf("backend %q", f.Backend)
			}
			msg := fmt.Sprintf("route: %s returned %d for %s", where, f.Status, f.To)
			if f.From != f.To {
				msg += fmt.Sprintf(" (routed from %s)", f.From)
			}
			if f.Body != "" {
				msg += ": " + f.Body
			}
			errorf("%s", msg)
		}
	}

	var extra []proxy.RawMiddleware
	if router != nil {
		extra = append(extra, proxy.RoutingMiddleware(router))
	}

	// ListenAddr is left unset: proxy.New binds 127.0.0.1 on an ephemeral
	// port, which was the only address config was ever allowed to name.
	// A pinned port exists for an agent steered by a config file the user
	// wrote by hand: that file names a URL, so the port cannot be the
	// ephemeral one omni would otherwise pick per launch. Host is never
	// configurable — see applyListenPort.
	listenAddr := ""
	if eff.ListenPort.V != 0 {
		listenAddr = fmt.Sprintf("127.0.0.1:%d", eff.ListenPort.V)
	}
	srv, err := proxy.New(proxy.Config{
		Upstream:        up,
		Recorder:        rec,
		ExtraMiddleware: extra,
		ListenAddr:      listenAddr,
	})
	if err != nil {
		errorf("cannot create proxy: %v", err)
		return exitUnavailable
	}
	if err := srv.Start(); err != nil {
		errorf("cannot start proxy: %v", err)
		return exitUnavailable
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if verbose {
		errorf("proxy listening on %s -> %s", srv.Addr(), upstream)
		if rec != nil {
			errorf("recording to %s", rec.Dir())
		}
	}

	// Tier 2 is not implemented yet; no CA path is injected.
	env := childEnv(os.Environ(), eff.Env.V, p.Env(srv.URL(), ""))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var diag *os.File
	if verbose {
		diag = os.Stderr
	}

	res, err := launcher.Run(ctx, launcher.Spec{
		Path:        binPath,
		Args:        inv.AgentArgs, // verbatim, never interpreted
		Env:         env,
		Diagnostics: diag,
	})
	if err != nil {
		errorf("running %s: %v", p.Name, err)
		return exitSoftware
	}
	return res.ExitCode
}

// childEnv builds the environment for the agent process: the inherited
// environment, then the user's [env] additions, then omni's steering vars.
//
// The order is the mechanism, not a style choice. os/exec resolves a
// duplicate key to its last occurrence, so appending the steering vars last
// is the only thing keeping a user [env] entry from pointing the agent at
// something other than the proxy — which would not fail loudly, it would
// quietly produce a session omni believes it is intercepting and is not.
func childEnv(base []string, userEnv map[string]string, steering []string) []string {
	env := make([]string, 0, len(base)+len(userEnv)+len(steering))
	env = append(env, base...)
	for k, v := range userEnv {
		env = append(env, k+"="+v)
	}
	return append(env, steering...)
}

// printCredentialSources reports where each routed backend's key comes from,
// by name and never by value.
//
// A dry run that listed the routes but not their credentials would print a
// plan that a real launch then refuses to carry out — resolveRouter treats a
// missing key as fatal once a rule targets the backend. Since a key may now
// come from either the environment or ~/.omni/credentials, "which one am I
// actually getting?" is a question --dry-run is the natural place to answer.
func printCredentialSources(out io.Writer, eff *config.Effective, rules []config.ResolvedRule) {
	seen := map[string]bool{}
	var lines []string
	for _, r := range rules {
		b := r.Backend
		if b == nil || b.APIKeyEnv == "" || seen[b.Name] {
			continue
		}
		seen[b.Name] = true
		if _, source, ok := eff.SecretFor(b.APIKeyEnv); ok {
			// SecretFor reports "$NAME" for the environment and a path for
			// the credentials file; say which in words rather than printing
			// the variable's name twice.
			from := source
			if source == "$"+b.APIKeyEnv {
				from = "the environment"
			}
			lines = append(lines, fmt.Sprintf("  %s: $%s from %s", b.Name, b.APIKeyEnv, from))
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"  %s: $%s is set neither in the environment nor in %s — this route would fail to launch",
			b.Name, b.APIKeyEnv, eff.CredentialsPathForMessage(),
		))
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(out, "credentials:\n")
	for _, l := range lines {
		fmt.Fprintln(out, l)
	}
}

// dryRun prints what would happen without launching anything.
// See internal-docs/09-cli-design.md §6.
func dryRun(inv *Invocation, p *profile.Profile, eff *config.Effective, binPath string) int {
	out := os.Stdout // requested output, not a diagnostic
	fmt.Fprintf(out, "would launch: %s\n", binPath)
	if len(inv.AgentArgs) > 0 {
		fmt.Fprintf(out, "with args:    %v\n", inv.AgentArgs)
	}
	fmt.Fprintf(out, "with env:\n")
	for _, e := range p.Env("http://127.0.0.1:<ephemeral>", "") {
		fmt.Fprintf(out, "  %s\n", e)
	}
	fmt.Fprintf(out, "config:\n")
	for _, r := range eff.Rows() {
		fmt.Fprintf(out, "  %-22s %-24s %s\n", r.Path, r.Value, r.Source)
	}
	if len(eff.Routes.V) > 0 {
		rules, issues := eff.Resolve(string(p.APIStyle))
		if eff.Mode.V == config.ModeOff {
			fmt.Fprintf(out, "routing (inactive — mode is \"off\"):\n")
		} else {
			fmt.Fprintf(out, "routing (first match wins):\n")
		}
		for _, r := range rules {
			fmt.Fprintf(out, "  %s\n", r)
		}
		for _, is := range issues {
			fmt.Fprintf(out, "  %s: %s\n", is.Level, is.Message)
		}
		printCredentialSources(out, eff, rules)
	}
	// Only advertise a session path when a session would actually be
	// written. Printing one unconditionally reads as a promise that this run
	// leaves a recording behind, which since recording became opt-in it does
	// not.
	switch {
	case eff.Mode.V == config.ModeOff:
		fmt.Fprintf(out, "recording: off (mode is \"off\")\n")
	case !eff.RecordEnabled.V:
		fmt.Fprintf(out, "recording: off — enable with --record, or record.enabled in config\n")
	default:
		if home, err := config.Home(); err == nil {
			fmt.Fprintf(out, "recording -> %s\n", filepath.Join(home, "sessions"))
		}
	}
	return exitOK
}
