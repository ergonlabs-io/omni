package config

import (
	"errors"
	"fmt"
	"os"
)

// Load resolves the effective configuration for agent by applying layers
// 1-5 from internal-docs/08-configuration.md §Precedence, in order:
//
//  1. built-in defaults
//  2. ~/.omni/omni.conf [defaults]
//  3. ~/.omni/omni.conf [agents.<agent>]
//  4. ./.omni.conf (current working directory only — never an ancestor)
//  5. OMNI_* environment variables
//
// Layer 6 (CLI flags) is not applied here — call (*Effective).Override
// with the flags cmd/omni parsed.
//
// Load returns a non-nil error, in which case the returned *Effective must
// not be used to run the proxy, in exactly one case: some layer contains a
// credential-shaped value. That is internal-docs/08-configuration.md
// §Security's "no credentials in config, ever", and this package enforces it
// at Load rather than only at `omni config check`. Every other problem — an
// unknown key, a disallowed project-config key, a mode typo — is recorded in
// Effective.Issues at LevelError or LevelWarning and does not stop Load;
// that is what `omni config check` reports.
func Load(agent string) (*Effective, error) {
	home, err := Home()
	if err != nil {
		return nil, err
	}
	return LoadFrom(home, agent)
}

// LoadFrom is Load with an explicit home directory, mainly for tests (which
// must point $OMNI_HOME at a t.TempDir() rather than call this directly —
// see the package tests for why: Load must go through Home() so it behaves
// exactly as it will for real users).
func LoadFrom(home, agent string) (*Effective, error) {
	e := builtinDefaults(agent)

	if err := loadGlobalLayer(e, home, agent); err != nil {
		return nil, err
	}
	if err := loadProjectLayer(e); err != nil {
		return nil, err
	}
	loadEnvLayer(e, agent)

	runChecks(e)

	if e.HasFatal() {
		return e, fmt.Errorf("config: refusing to load: %s", firstFatal(e.allIssues()))
	}
	return e, nil
}

func loadGlobalLayer(e *Effective, home, agent string) error {
	path := GlobalConfigPath(home)
	g, fl, err := loadGlobal(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("config: %s: %w", path, err)
	}
	applyDefaults(e, g.Defaults, func(p string) string { return fl.src("defaults." + p) }, &e.Issues)
	// Backends are global-only: no agent, project, or env layer declares
	// them. See Backend's doc comment for why a repo-local file must not.
	applyBackends(e, g.Backends, fl.src, &e.Issues)
	if av, ok := g.Agents[agent]; ok {
		applyAgent(e, av, func(p string) string { return fl.src("agents." + agent + "." + p) }, &e.Issues)
	}
	e.Issues = append(e.Issues, fl.issues...)
	return nil
}

func loadProjectLayer(e *Effective) error {
	cwd, err := os.Getwd()
	if err != nil {
		// Can't determine the project directory; skip this layer rather
		// than fail the whole load over it.
		return nil
	}
	path := ProjectConfigPath(cwd)
	r, fl, err := loadProjectConfig(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("config: %s: %w", path, err)
	}
	applyAgent(e, r, func(p string) string { return fl.src(p) }, &e.Issues)
	e.Issues = append(e.Issues, fl.issues...)
	return nil
}

func loadEnvLayer(e *Effective, agent string) {
	d, a, srcD, srcA, issues := envLayers(agent)
	applyDefaults(e, d, func(p string) string { return srcD[p] }, &e.Issues)
	applyAgent(e, a, func(p string) string { return srcA[p] }, &e.Issues)
	e.Issues = append(e.Issues, issues...)
}

func firstFatal(issues []Issue) Issue {
	for _, is := range issues {
		if is.Level == LevelFatal {
			return is
		}
	}
	return Issue{}
}

// Override applies layer 6 (CLI flags) on top of an already-loaded
// Effective. keys are dotted config paths in the same namespace as the file
// layers (e.g. "mode", "redact"); source labels
// every value it sets, e.g. "(cli flag)".
//
// This is the hook cmd/omni uses: parse its own flags, translate them to
// this map, and call Override once after Load.
func (e *Effective) Override(overrides map[string]string, source string) error {
	if len(overrides) == 0 {
		return nil
	}
	var d rawDefaults
	var unknown []string
	for path, v := range overrides {
		val := v
		known, err := setRawDefaultsField(&d, path, val)
		if err != nil {
			return fmt.Errorf("config: override %s=%q: %w", path, v, err)
		}
		if !known {
			unknown = append(unknown, path)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("config: unknown override key(s): %v", unknown)
	}
	applyDefaults(e, d, func(string) string { return source }, &e.Issues)
	runChecks(e)
	if e.HasFatal() {
		return fmt.Errorf("config: refusing override: %s", firstFatal(e.allIssues()))
	}
	return nil
}
