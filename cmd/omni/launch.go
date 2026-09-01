package main

import (
	"context"
	"fmt"
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

	// Recorder. Failing to record must never become the user's outage
	// (internal-docs/05-constraints.md §5), so a recorder that cannot be
	// created downgrades to no recording rather than aborting the session.
	var rec *record.Recorder
	if eff.Record.Enabled.V && eff.Mode.V != config.ModeOff {
		home, herr := config.Home()
		if herr != nil {
			errorf("warning: cannot resolve omni home, recording disabled: %v", herr)
		} else {
			rec, err = record.New(
				filepath.Join(home, "sessions"), p.Name, version,
				record.WithRedaction(eff.Record.Redact.V),
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
	}

	var extra []proxy.RawMiddleware
	if router != nil {
		extra = append(extra, proxy.RoutingMiddleware(router))
	}

	srv, err := proxy.New(proxy.Config{
		Upstream:        up,
		Recorder:        rec,
		ListenAddr:      eff.Proxy.Listen.V,
		ExtraMiddleware: extra,
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
		if eff.Mode.V == config.ModeRoute {
			fmt.Fprintf(out, "routing (first match wins):\n")
		} else {
			fmt.Fprintf(out, "routing (inactive — mode is %q, not \"route\"):\n", eff.Mode.V)
		}
		for _, r := range rules {
			fmt.Fprintf(out, "  %s\n", r)
		}
		for _, is := range issues {
			fmt.Fprintf(out, "  %s: %s\n", is.Level, is.Message)
		}
	}
	if home, err := config.Home(); err == nil {
		fmt.Fprintf(out, "sessions -> %s\n", filepath.Join(home, "sessions"))
	}
	return exitOK
}
