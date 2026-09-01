package config

import (
	"os"
	"path/filepath"
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
