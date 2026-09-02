package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testHome points $OMNI_HOME at a not-yet-existing "omni-home" directory
// inside a fresh t.TempDir(), for the duration of the test, and returns
// that path. It deliberately does not create the directory itself: real
// ~/.omni does not exist on first run either, and Init's permission
// bootstrapping (0700 on creation) can only be exercised faithfully against
// a path nothing has created yet. Every test in this package must go
// through this (or otherwise set $OMNI_HOME itself) — see the task's hard
// requirement that config tests never touch a real ~/.omni.
func testHome(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "omni-home")
	t.Setenv(HomeEnvVar, dir)
	return dir
}

// testCWD chdirs the test into a fresh, empty t.TempDir() so the project
// config layer (./.omni.conf) does not accidentally pick up a stray file
// from wherever `go test` happens to run. Returns the directory so callers
// can write a .omni.conf into it.
func testCWD(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

// writeTestFile creates path (and its parent directories) with content.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeAgentSection writes body — an agent-shaped TOML fragment with flat
// keys, the way a per-agent config reads on its own — into omni.conf's
// [agents.<agent>] table, rewriting each table header into that namespace
// ([record] -> [agents.claude.record], [[route]] ->
// [[agents.claude.route]]).
//
// This exists because the per-agent layer is now a section of omni.conf
// rather than its own ~/.omni/agents/<name>.conf file. Keeping the fragments
// flat at the call site keeps these tests about the per-agent *layer*
// rather than about TOML nesting.
func writeAgentSection(t *testing.T, home, agent, body string) {
	t.Helper()
	var bare, tables []string
	inTable := false
	for _, ln := range strings.Split(body, "\n") {
		switch trimmed := strings.TrimSpace(ln); {
		case strings.HasPrefix(trimmed, "[["):
			inTable = true
			tables = append(tables, "[[agents."+agent+"."+strings.TrimPrefix(trimmed, "[["))
		case strings.HasPrefix(trimmed, "["):
			inTable = true
			tables = append(tables, "[agents."+agent+"."+strings.TrimPrefix(trimmed, "["))
		case inTable:
			tables = append(tables, ln)
		default:
			bare = append(bare, ln)
		}
	}
	out := "[agents." + agent + "]\n" + strings.Join(bare, "\n") + "\n" + strings.Join(tables, "\n")
	writeTestFile(t, GlobalConfigPath(home), out)
}
