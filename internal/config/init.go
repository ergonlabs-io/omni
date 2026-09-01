package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed templates/omni.conf.tmpl
var omniConfTemplate []byte

//go:embed templates/agents_claude.conf.tmpl
var agentsClaudeTemplate []byte

//go:embed templates/agents_codex.conf.tmpl
var agentsCodexTemplate []byte

// dirPerm and homePerm/caPerm implement the exact permissions
// internal-docs/08-configuration.md §Bootstrap calls for: "~/.omni 0700,
// ca/ 0700" set explicitly rather than relying on umask. Directories that
// hold nothing sensitive on their own (agents/, profiles.d/, cache/,
// sessions/) use the ordinary 0755 — the home directory's 0700 already
// blocks other users from traversing into them at all.
const (
	homePerm = 0o700
	caPerm   = 0o700
	dirPerm  = 0o755
	filePerm = 0o644
)

// Init creates the omni home tree under home and writes fully-commented
// default config files, matching the layout in
// internal-docs/08-configuration.md:
//
//	~/.omni/
//	├── omni.conf
//	├── agents/{claude,codex}.conf
//	├── profiles.d/
//	├── ca/                (created empty — the CA itself is generated
//	│                        lazily on first --all-traffic, never here)
//	├── cache/
//	└── sessions/
//
// Init is idempotent: it never overwrites or removes an existing file or
// directory. It returns the absolute paths of everything it newly created,
// in creation order, so a caller (`omni init`) can report exactly what
// happened. Safe to call on every `omni claude` invocation for
// auto-bootstrap — a repeat call against an already-initialized home
// creates nothing and returns an empty slice.
func Init(home string) ([]string, error) {
	var created []string

	mkdir := func(path string, perm os.FileMode) error {
		if _, err := os.Stat(path); err == nil {
			return nil // already exists — leave it alone
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(path, perm); err != nil {
			return err
		}
		created = append(created, path)
		return nil
	}

	writeFile := func(path string, content []byte, perm os.FileMode) error {
		if _, err := os.Stat(path); err == nil {
			return nil // never clobber an existing file
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(path, content, perm); err != nil {
			return err
		}
		created = append(created, path)
		return nil
	}

	if err := mkdir(home, homePerm); err != nil {
		return created, fmt.Errorf("config: init %s: %w", home, err)
	}
	// Directory permissions are not retroactively tightened on an
	// already-existing home — Init only sets them at creation time, per
	// spec. (An existing world-readable ~/.omni is left as the user made
	// it; Init does not silently rewrite permissions out from under them.)

	agentsDir := filepath.Join(home, "agents")
	profilesDir := filepath.Join(home, "profiles.d")
	caDir := filepath.Join(home, "ca")
	cacheDir := filepath.Join(home, "cache")
	sessionsDir := filepath.Join(home, "sessions")

	for _, d := range []struct {
		path string
		perm os.FileMode
	}{
		{agentsDir, dirPerm},
		{profilesDir, dirPerm},
		{caDir, caPerm}, // 0700 — will hold ca.pem / ca-key.pem once generated lazily
		{cacheDir, dirPerm},
		{sessionsDir, dirPerm},
	} {
		if err := mkdir(d.path, d.perm); err != nil {
			return created, fmt.Errorf("config: init %s: %w", d.path, err)
		}
	}

	for _, f := range []struct {
		path    string
		content []byte
	}{
		{GlobalConfigPath(home), omniConfTemplate},
		{AgentConfigPath(home, "claude"), agentsClaudeTemplate},
		{AgentConfigPath(home, "codex"), agentsCodexTemplate},
	} {
		if err := writeFile(f.path, f.content, filePerm); err != nil {
			return created, fmt.Errorf("config: init %s: %w", f.path, err)
		}
	}

	return created, nil
}
