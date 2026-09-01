// Package config implements omni's layered configuration system: the
// ~/.omni tree, omni.conf (TOML), per-agent overrides, project-local
// config, and environment variables, merged into one effective,
// provenance-annotated configuration per agent.
//
// See internal-docs/08-configuration.md for the full specification this
// package implements.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// HomeEnvVar is the environment variable that overrides omni's root
// directory. Set by tests (pointed at t.TempDir()) and by anyone who wants
// omni's state somewhere other than ~/.omni.
const HomeEnvVar = "OMNI_HOME"

// Home resolves omni's root directory: $OMNI_HOME if set (even to a
// relative or nonexistent path — callers decide whether to create it), else
// ~/.omni.
//
// Home never creates the directory; use Init for that.
func Home() (string, error) {
	if h := os.Getenv(HomeEnvVar); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".omni"), nil
}

// GlobalConfigPath returns the path to the global omni.conf under home.
func GlobalConfigPath(home string) string {
	return filepath.Join(home, "omni.conf")
}

// AgentConfigPath returns the path to the per-agent drop-in config for
// agent under home.
func AgentConfigPath(home, agent string) string {
	return filepath.Join(home, "agents", agent+".conf")
}

// ProjectConfigName is the filename of the per-project, repo-local config.
// It is read from the current working directory only — never from parent
// directories. See internal-docs/08-configuration.md §Precedence.
const ProjectConfigName = ".omni.conf"

// ProjectConfigPath returns the path to the project-local config in dir
// (normally the current working directory).
func ProjectConfigPath(dir string) string {
	return filepath.Join(dir, ProjectConfigName)
}
