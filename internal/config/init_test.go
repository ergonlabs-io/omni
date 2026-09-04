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

	// Nothing is created for a feature that does not exist. cache/ was for a
	// Models API capability cache and ca/ for Tier 2's certificate
	// authority; no code read or wrote either. They return with the code
	// that needs them.
	for _, d := range []string{"ca", "cache"} {
		if _, err := os.Stat(filepath.Join(home, d)); !os.IsNotExist(err) {
			t.Errorf("%s/ was created, but nothing uses it", d)
		}
	}

	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(home); err == nil {
			if perm := fi.Mode().Perm(); perm != privatePerm {
				t.Errorf("home perm = %o, want %o", perm, privatePerm)
			}
		}
		if fi, err := os.Stat(filepath.Join(home, "sessions")); err == nil {
			if perm := fi.Mode().Perm(); perm != privatePerm {
				t.Errorf("sessions/ perm = %o, want %o", perm, privatePerm)
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

// TestInitCreatesCredentialsAt0600 pins the reason the file ships at all.
// A credentials file that anyone else can read is refused outright
// (LevelFatal), and the user who creates it by hand is doing so at the
// moment they first have a key to put in it — which is the worst moment to
// meet a permission error. Shipping it pre-created at the right mode makes
// uncommenting a line the whole procedure.
func TestInitCreatesCredentialsAt0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	home := testHome(t)
	if _, err := Init(home); err != nil {
		t.Fatalf("Init: %v", err)
	}
	fi, err := os.Stat(CredentialsPath(home))
	if err != nil {
		t.Fatalf("credentials file not created: %v", err)
	}
	// Stricter is fine (a tight umask); looser is the whole bug.
	if perm := fi.Mode().Perm(); perm&^credentialsPerm != 0 {
		t.Errorf("credentials is %04o, want %04o or stricter", perm, credentialsPerm)
	}
}

// TestInitNeverClobbersCredentials is the one that would actually hurt.
// Init is safe to call on every launch for auto-bootstrap, so if it
// rewrote this file it would silently destroy a real key on the next
// `omni claude`.
func TestInitNeverClobbersCredentials(t *testing.T) {
	home := testHome(t)
	if _, err := Init(home); err != nil {
		t.Fatalf("Init: %v", err)
	}
	const secret = "OPENROUTER_API_KEY=a-real-key\n"
	if err := os.WriteFile(CredentialsPath(home), []byte(secret), credentialsPerm); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Init(home); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	got, err := os.ReadFile(CredentialsPath(home))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != secret {
		t.Fatalf("Init overwrote a credentials file holding a key:\n%s", got)
	}
}

// TestInitCredentialsLoadClean checks the shipped file is not merely
// present but inert: comments only, so it parses to an empty set with no
// issues, and the example key in it does not trip the credential-shaped
// value detector that guards config.
func TestInitCredentialsLoadClean(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	if _, err := Init(home); err != nil {
		t.Fatalf("Init: %v", err)
	}
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load on a freshly initialized home: %v", err)
	}
	for _, is := range e.Check() {
		t.Errorf("fresh home is not clean: %s", is)
	}
	if names := e.creds.Names(); len(names) != 0 {
		t.Errorf("shipped credentials defines %v, want nothing until the user edits it", names)
	}
}
