package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ergonlabs-io/omni/internal/config"
	"github.com/ergonlabs-io/omni/internal/profile"
)

// routerTestHome points $OMNI_HOME at a fresh directory holding conf, and
// returns the path. It also clears OPENROUTER_API_KEY, because a developer
// running these tests plausibly has it exported — which is exactly the
// variable under test.
func routerTestHome(t *testing.T, conf string) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "omni-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "omni.conf"), []byte(conf), 0o600); err != nil {
		t.Fatalf("write omni.conf: %v", err)
	}
	t.Setenv(config.HomeEnvVar, home)
	t.Setenv("OPENROUTER_API_KEY", "")
	if err := os.Unsetenv("OPENROUTER_API_KEY"); err != nil {
		t.Fatalf("unset: %v", err)
	}
	t.Chdir(t.TempDir()) // no stray ./.omni.conf
	return home
}

const routerTestConf = `
[defaults]
mode = "record"

[backends.openrouter]
base_url    = "https://openrouter.ai/api"
api_key_env = "OPENROUTER_API_KEY"
api_style   = "anthropic"
model       = "minimax/minimax-m3:free"

[[agents.claude.route]]
match   = "claude-haiku-4-5*"
backend = "openrouter"
`

func writeCreds(t *testing.T, home, body string) {
	t.Helper()
	path := filepath.Join(home, "credentials")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod credentials: %v", err)
	}
}

// TestResolveRouterUsesCredentialsFile is the launch-path half of the
// feature: a key that exists only in ~/.omni/credentials must be good enough
// to build the backend, since that is the whole reason to write one.
func TestResolveRouterUsesCredentialsFile(t *testing.T) {
	home := routerTestHome(t, routerTestConf)
	writeCreds(t, home, "OPENROUTER_API_KEY=key-from-file\n")

	eff, err := config.LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := profile.Lookup("claude")
	router, err := resolveRouter(eff, p)
	if err != nil {
		t.Fatalf("resolveRouter: %v", err)
	}
	if router == nil {
		t.Fatal("no router built")
	}

	// The router keeps its rules private, so assert on the function that
	// actually makes the credential decision.
	b, err := buildBackend(eff.Backends.V["openrouter"], p.Upstream, eff)
	if err != nil {
		t.Fatalf("buildBackend: %v", err)
	}
	if b.APIKey != "key-from-file" {
		t.Errorf("APIKey = %q, want the file's value", b.APIKey)
	}
	if b.PreserveAuth {
		t.Error("PreserveAuth set for a third-party backend")
	}
}

// TestResolveRouterMissingCredentialNamesBothSources pins the error text. A
// user who hits this needs to know there are two places to fix it and where
// the second one lives; "export it" alone is what sent people to paste the
// key into omni.conf in the first place.
func TestResolveRouterMissingCredentialNamesBothSources(t *testing.T) {
	home := routerTestHome(t, routerTestConf)

	eff, err := config.LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = resolveRouter(eff, profile.Lookup("claude"))
	if err == nil {
		t.Fatal("resolveRouter succeeded with no credential anywhere")
	}
	msg := err.Error()
	for _, want := range []string{
		"OPENROUTER_API_KEY",
		filepath.Join(home, "credentials"),
		"chmod 600",
		"openrouter",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not mention %q: %s", want, msg)
		}
	}
}

// TestPrintCredentialSources covers what --dry-run reports. A dry run that
// listed routes without their credentials would print a plan that
// resolveRouter then refuses to carry out.
func TestPrintCredentialSources(t *testing.T) {
	home := routerTestHome(t, routerTestConf)
	p := profile.Lookup("claude")

	render := func(t *testing.T) string {
		t.Helper()
		eff, err := config.LoadFrom(home, "claude")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		rules, _ := eff.Resolve(string(p.APIStyle))
		var buf bytes.Buffer
		printCredentialSources(&buf, eff, rules)
		return buf.String()
	}

	t.Run("from the file", func(t *testing.T) {
		writeCreds(t, home, "OPENROUTER_API_KEY=key-from-file\n")
		out := render(t)
		if !strings.Contains(out, filepath.Join(home, "credentials")) {
			t.Errorf("does not name the credentials file:\n%s", out)
		}
		if strings.Contains(out, "key-from-file") {
			t.Errorf("printed the secret:\n%s", out)
		}
	})

	t.Run("from the environment", func(t *testing.T) {
		writeCreds(t, home, "OPENROUTER_API_KEY=key-from-file\n")
		t.Setenv("OPENROUTER_API_KEY", "key-from-env")
		out := render(t)
		if !strings.Contains(out, "from the environment") {
			t.Errorf("does not say the environment won:\n%s", out)
		}
		if strings.Contains(out, "key-from-env") || strings.Contains(out, "key-from-file") {
			t.Errorf("printed a secret:\n%s", out)
		}
	})

	t.Run("from neither", func(t *testing.T) {
		if err := os.Remove(filepath.Join(home, "credentials")); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove: %v", err)
		}
		out := render(t)
		if !strings.Contains(out, "would fail to launch") {
			t.Errorf("does not warn that the route cannot launch:\n%s", out)
		}
	})
}
