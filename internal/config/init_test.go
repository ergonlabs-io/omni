package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInitCreatesTree(t *testing.T) {
	home := testHome(t)

	created, err := Init(home)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(created) == 0 {
		t.Fatalf("Init created nothing on a fresh home")
	}

	wantFiles := []string{
		GlobalConfigPath(home),
	}
	for _, p := range wantFiles {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}

	wantDirs := []string{
		filepath.Join(home, "profiles.d"),
		filepath.Join(home, "ca"),
		filepath.Join(home, "cache"),
		filepath.Join(home, "sessions"),
	}
	for _, d := range wantDirs {
		fi, err := os.Stat(d)
		if err != nil {
			t.Errorf("expected dir %s to exist: %v", d, err)
			continue
		}
		if !fi.IsDir() {
			t.Errorf("%s exists but is not a directory", d)
		}
	}

	// The CA cert/key themselves must NOT be generated at init — only the
	// directory. Tier 2 CA generation is lazy, on first --all-traffic.
	caDir := filepath.Join(home, "ca")
	entries, err := os.ReadDir(caDir)
	if err != nil {
		t.Fatalf("read ca dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("ca/ should be empty after Init, got: %v", entries)
	}

	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(home); err == nil {
			if perm := fi.Mode().Perm(); perm != privatePerm {
				t.Errorf("home perm = %o, want %o", perm, privatePerm)
			}
		}
		if fi, err := os.Stat(caDir); err == nil {
			if perm := fi.Mode().Perm(); perm != privatePerm {
				t.Errorf("ca/ perm = %o, want %o", perm, privatePerm)
			}
		}
	}
}

func TestInitIsIdempotent(t *testing.T) {
	home := testHome(t)

	if _, err := Init(home); err != nil {
		t.Fatalf("first Init: %v", err)
	}

	// Modify the generated file so we can tell whether a second Init
	// touches it. omni.conf is the one file Init writes, which makes it the
	// real test of "never clobber".
	customContent := "# customized by the user\n[defaults]\nmode = \"route\"\n"
	conf := GlobalConfigPath(home)
	if err := os.WriteFile(conf, []byte(customContent), 0o644); err != nil {
		t.Fatalf("customize omni.conf: %v", err)
	}

	created, err := Init(home)
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if len(created) != 0 {
		t.Errorf("second Init on an already-initialized home created: %v, want nothing", created)
	}

	got, err := os.ReadFile(conf)
	if err != nil {
		t.Fatalf("read omni.conf: %v", err)
	}
	if string(got) != customContent {
		t.Errorf("Init clobbered a user-modified file; got %q, want %q", got, customContent)
	}
}

// TestInitBootstrappedConfigLoads checks that the files Init writes are
// themselves valid config that LoadFrom can parse without error — i.e. the
// generated defaults are not just documentation, they work.
func TestInitBootstrappedConfigLoads(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	if _, err := Init(home); err != nil {
		t.Fatalf("Init: %v", err)
	}

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("LoadFrom after Init: %v (issues: %+v)", err, e.Check())
	}
	for _, is := range e.Check() {
		if is.Level == LevelError {
			t.Errorf("bootstrapped config has an error-level issue: %s", is)
		}
	}
	// The generated [agents.claude] section overrides nothing: a fresh
	// install must not change how the agent behaves, and `omni init` never
	// rewrites omni.conf once it exists, so anything active there is
	// permanent for that user.
	if e.Mode.V != ModeRecord {
		t.Errorf("claude mode = %q, want record (inherited; the generated [agents.claude] must not override it)", e.Mode.V)
	}
	if len(e.Routes.V) != 0 {
		t.Errorf("bootstrapped claude config has live routing rules %v; the generated template must ship none", e.Routes.V)
	}

	eCodex, err := LoadFrom(home, "codex")
	if err != nil {
		t.Fatalf("LoadFrom codex after Init: %v", err)
	}
	if eCodex.Mode.V != ModeRecord {
		t.Errorf("codex mode = %q, want record", eCodex.Mode.V)
	}
}
