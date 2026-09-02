package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeCredentials writes body to home's credentials file with mode perm,
// creating home if needed. It chmods explicitly after the write because
// os.WriteFile's mode is masked by the process umask, and these tests are
// about the exact mode on disk.
func writeCredentials(t *testing.T, home, body string, perm os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", home, err)
	}
	path := CredentialsPath(home)
	if err := os.WriteFile(path, []byte(body), perm); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	return path
}

// issuesAt returns the issues at one level, for assertions about severity
// rather than about the whole list.
func issuesAt(issues []Issue, level Level) []Issue {
	var out []Issue
	for _, is := range issues {
		if is.Level == level {
			out = append(out, is)
		}
	}
	return out
}

// TestCredentialsMissingFileIsSilent pins the common case: no credentials
// file at all. The file is optional — exporting the variable in a shell
// remains a first-class way to supply a key — so its absence must not
// produce so much as a warning.
func TestCredentialsMissingFileIsSilent(t *testing.T) {
	home := testHome(t)
	testCWD(t)

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, is := range e.allIssues() {
		if strings.HasPrefix(is.Path, "credentials") {
			t.Errorf("unexpected issue for a missing credentials file: %+v", is)
		}
	}
	if got := e.CredentialsPath(); got != CredentialsPath(home) {
		t.Errorf("CredentialsPath() = %q, want %q", got, CredentialsPath(home))
	}
	if _, _, ok := e.SecretFor("ANYTHING"); ok {
		t.Error("SecretFor resolved a secret with no file and no environment")
	}
}

// TestCredentialsSuppliesBackendKey is the whole point of the feature: a key
// that exists only in the credentials file resolves for a backend naming it,
// and no warning is emitted about that key being unset.
func TestCredentialsSuppliesBackendKey(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	testUnsetEnv(t, "OPENROUTER_API_KEY")
	writeCredentials(t, home, "OPENROUTER_API_KEY=or-secret-value\n", 0o600)
	writeTestFile(t, GlobalConfigPath(home), `
[backends.openrouter]
base_url = "https://openrouter.ai/api"
api_key_env = "OPENROUTER_API_KEY"
`)

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	val, source, ok := e.SecretFor("OPENROUTER_API_KEY")
	if !ok || val != "or-secret-value" {
		t.Fatalf("SecretFor = (%q, %q, %v), want the file's value", val, source, ok)
	}
	if source != CredentialsPath(home) {
		t.Errorf("source = %q, want the credentials path", source)
	}
	for _, is := range e.allIssues() {
		if strings.Contains(is.Message, "OPENROUTER_API_KEY") {
			t.Errorf("still warned about a key the file supplies: %+v", is)
		}
	}
}

// TestCredentialsEnvironmentWins fixes precedence between the two sources.
// The environment is the more explicit, more ephemeral act — it is how you
// override the stored key for one run — so it wins.
func TestCredentialsEnvironmentWins(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeCredentials(t, home, "OPENROUTER_API_KEY=from-file\n", 0o600)
	t.Setenv("OPENROUTER_API_KEY", "from-env")

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	val, source, ok := e.SecretFor("OPENROUTER_API_KEY")
	if !ok || val != "from-env" {
		t.Fatalf("SecretFor = (%q, %v), want from-env", val, ok)
	}
	if source != "$OPENROUTER_API_KEY" {
		t.Errorf("source = %q, want $OPENROUTER_API_KEY", source)
	}
}

// TestCredentialsLoosePermissionsAreFatal covers the file's one hard rule. A
// credentials file others can read is refused rather than read: reading it
// would leave the key exposed while teaching the user that the mode does not
// matter. Fatal means Load itself fails, exactly as for a credential pasted
// into omni.conf.
func TestCredentialsLoosePermissionsAreFatal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	home := testHome(t)
	testCWD(t)
	testUnsetEnv(t, "OPENROUTER_API_KEY")
	path := writeCredentials(t, home, "OPENROUTER_API_KEY=leaky\n", 0o644)

	e, err := LoadFrom(home, "claude")
	if err == nil {
		t.Fatal("Load succeeded with a world-readable credentials file")
	}
	if !e.HasFatal() {
		t.Error("no Fatal issue recorded")
	}
	fatal := issuesAt(e.allIssues(), LevelFatal)
	if len(fatal) != 1 {
		t.Fatalf("got %d fatal issues, want 1: %+v", len(fatal), fatal)
	}
	msg := fatal[0].Message
	if !strings.Contains(msg, "chmod 0600") || !strings.Contains(msg, path) {
		t.Errorf("message does not say how to fix it: %q", msg)
	}
	if strings.Contains(msg, "leaky") {
		t.Errorf("issue message leaked the secret: %q", msg)
	}
	// Refused, not read.
	if _, _, ok := e.SecretFor("OPENROUTER_API_KEY"); ok {
		t.Error("read a credentials file it had already refused")
	}
}

// TestCredentialsReadOnlyIsAccepted guards the permission check against
// being written as an equality test. 0400 is stricter than 0600, not looser,
// and a user who chmods their secrets read-only should not be told off.
func TestCredentialsReadOnlyIsAccepted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	home := testHome(t)
	testCWD(t)
	writeCredentials(t, home, "K=v\n", 0o400)

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if val, _, ok := e.SecretFor("K"); !ok || val != "v" {
		t.Errorf("SecretFor = (%q, %v), want v", val, ok)
	}
}

// TestCredentialsParsing walks the line shapes the file accepts, including
// the ones people paste straight out of a shell rc.
func TestCredentialsParsing(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeCredentials(t, home, strings.Join([]string{
		"# a comment",
		"",
		"   ",
		"PLAIN=abc123",
		"export EXPORTED=def456",
		`DQUOTED="ghi789"`,
		"SQUOTED='jkl012'",
		"  SPACED  =  mno345  ",
		"EMPTY=",
		"WITH_EQUALS=a=b=c",
		`BACKSLASH=a\nb`,
	}, "\n")+"\n", 0o600)

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, is := range e.allIssues() {
		if strings.HasPrefix(is.Path, "credentials") {
			t.Errorf("unexpected issue: %+v", is)
		}
	}
	want := map[string]string{
		"PLAIN":       "abc123",
		"EXPORTED":    "def456",
		"DQUOTED":     "ghi789",
		"SQUOTED":     "jkl012",
		"SPACED":      "mno345",
		"EMPTY":       "",
		"WITH_EQUALS": "a=b=c",
		"BACKSLASH":   `a\nb`,
	}
	for name, wantVal := range want {
		got, ok := e.creds.Lookup(name)
		if !ok {
			t.Errorf("%s: not parsed", name)
			continue
		}
		if got != wantVal {
			t.Errorf("%s = %q, want %q", name, got, wantVal)
		}
	}
	// An empty value is present but is not a credential: a backend naming it
	// must still be reported as having no key.
	if _, _, ok := e.SecretFor("EMPTY"); ok {
		t.Error("an empty value resolved as a secret")
	}
}

// TestCredentialsMalformedLines checks that a bad line is reported with its
// line number, does not abort the rest of the file, and never quotes the
// value it failed to parse.
func TestCredentialsMalformedLines(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	path := writeCredentials(t, home, strings.Join([]string{
		"GOOD=fine",
		"this line has no equals sign",
		"2BAD=starts-with-a-digit",
		"has spaces=nope",
		"ALSO_GOOD=fine-too",
	}, "\n")+"\n", 0o600)

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	errs := issuesAt(e.allIssues(), LevelError)
	if len(errs) != 3 {
		t.Fatalf("got %d errors, want 3: %+v", len(errs), errs)
	}
	for i, wantLine := range []string{":2", ":3", ":4"} {
		if !strings.HasPrefix(errs[i].Source, path) || !strings.HasSuffix(errs[i].Source, wantLine) {
			t.Errorf("errs[%d].Source = %q, want %s%s", i, errs[i].Source, path, wantLine)
		}
	}
	// Parsing continues past a bad line.
	for _, name := range []string{"GOOD", "ALSO_GOOD"} {
		if _, ok := e.creds.Lookup(name); !ok {
			t.Errorf("%s was lost to an unrelated malformed line", name)
		}
	}
}

// TestCredentialsDuplicateWarns pins last-wins plus a warning. Silently
// picking one of two definitions is how you spend an afternoon wondering why
// the key you edited had no effect.
func TestCredentialsDuplicateWarns(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeCredentials(t, home, "K=first\nK=second\n", 0o600)

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	warns := issuesAt(e.allIssues(), LevelWarning)
	if len(warns) != 1 || warns[0].Path != "credentials.K" {
		t.Fatalf("warnings = %+v, want one about credentials.K", warns)
	}
	if strings.Contains(warns[0].Message, "first") || strings.Contains(warns[0].Message, "second") {
		t.Errorf("warning leaked a value: %q", warns[0].Message)
	}
	if val, _, _ := e.SecretFor("K"); val != "second" {
		t.Errorf("SecretFor = %q, want the last definition", val)
	}
}

// TestCredentialsDirectoryIsAnError covers the one non-permission stat
// failure worth naming, since ~/.omni/credentials/ is an easy thing to
// create by accident.
func TestCredentialsDirectoryIsAnError(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	if err := os.MkdirAll(CredentialsPath(home), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	errs := issuesAt(e.allIssues(), LevelError)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "directory") {
		t.Fatalf("errors = %+v, want one about a directory", errs)
	}
}

// TestCredentialsNamesNeverLeakValues pins the accessor that exists to be
// printed. Names are diagnostic; values never are.
func TestCredentialsNamesNeverLeakValues(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeCredentials(t, home, "ZED=z-secret\nALPHA=a-secret\n", 0o600)

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if names := e.creds.Names(); strings.Join(names, ",") != "ALPHA,ZED" {
		t.Errorf("Names() = %v, want sorted [ALPHA ZED]", names)
	}
}

// TestCredentialsNotVisibleInConfigShow is the containment test: the file is
// not a config layer, so nothing it holds may surface in `omni config show`,
// which people paste into bug reports.
func TestCredentialsNotVisibleInConfigShow(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	testUnsetEnv(t, "OPENROUTER_API_KEY")
	writeCredentials(t, home, "OPENROUTER_API_KEY=or-secret-value\n", 0o600)
	writeTestFile(t, GlobalConfigPath(home), `
[backends.openrouter]
base_url = "https://openrouter.ai/api"
api_key_env = "OPENROUTER_API_KEY"
`)

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out := e.Show(); strings.Contains(out, "or-secret-value") {
		t.Errorf("config show printed the secret:\n%s", out)
	}
}

// TestCredentialsPathIsUnderHome guards the path against drifting out of the
// omni home, whose 0700 mode is the second half of this file's protection.
func TestCredentialsPathIsUnderHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "omni-home")
	if got, want := CredentialsPath(home), filepath.Join(home, "credentials"); got != want {
		t.Errorf("CredentialsPath = %q, want %q", got, want)
	}
}
