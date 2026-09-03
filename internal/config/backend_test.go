package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// backendConf is a minimal omni.conf declaring one Anthropic-native
// backend and routing haiku to it — the shape this feature exists for.
// No mode is set: routing is on because rules exist.
const backendConf = `
[backends.openrouter]
base_url    = "https://openrouter.ai/api"
api_key_env = "OPENROUTER_API_KEY"
api_style   = "anthropic"
model       = "minimax/minimax-m3:free"

[backends.openrouter.headers]
X-Title = "omni"

[[agents.claude.route]]
match   = "claude-haiku-4-5*"
backend = "openrouter"
`

func loadWithGlobal(t *testing.T, conf string) *Effective {
	t.Helper()
	home := testHome(t)
	testCWD(t)
	t.Setenv("OPENROUTER_API_KEY", "or-test-key")
	writeTestFile(t, filepath.Join(home, "omni.conf"), conf)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	return e
}

// issueFor returns the first issue whose Path matches, or nil.
func issueFor(e *Effective, path string) *Issue {
	all := e.Check()
	for i := range all {
		if all[i].Path == path {
			return &all[i]
		}
	}
	return nil
}

func assertNoErrors(t *testing.T, e *Effective) {
	t.Helper()
	for _, is := range e.Check() {
		if is.Level == LevelError {
			t.Errorf("unexpected error issue: %s", is)
		}
	}
}

func TestBackendRouteResolves(t *testing.T) {
	e := loadWithGlobal(t, backendConf)
	assertNoErrors(t, e)

	rules, issues := e.Resolve("anthropic")
	if len(issues) != 0 {
		t.Fatalf("Resolve issues: %v", issues)
	}
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	r := rules[0]
	if r.Match != "claude-haiku-4-5*" {
		t.Errorf("Match = %q", r.Match)
	}
	// The rule names no model, so the backend's own model is used.
	if r.Model != "minimax/minimax-m3:free" {
		t.Errorf("Model = %q, want the backend's model", r.Model)
	}
	if !r.Rerouted() {
		t.Fatal("rule should name a backend")
	}
	if r.Backend.BaseURL != "https://openrouter.ai/api" {
		t.Errorf("BaseURL = %q", r.Backend.BaseURL)
	}
	if r.Backend.Headers["X-Title"] != "omni" {
		t.Errorf("headers = %v", r.Backend.Headers)
	}
}

// A rule's own model wins over the backend's default.
func TestRuleModelOverridesBackendModel(t *testing.T) {
	e := loadWithGlobal(t, `
[backends.openrouter]
base_url    = "https://openrouter.ai/api"
api_key_env = "OPENROUTER_API_KEY"
model       = "backend-default"

[[agents.claude.route]]
match   = "claude-haiku-*"
backend = "openrouter"
model   = "rule-specific"
`)
	rules, issues := e.Resolve("anthropic")
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	if rules[0].Model != "rule-specific" {
		t.Errorf("Model = %q, want the rule's own", rules[0].Model)
	}
}

// Rules keep the order they were written in — first match wins depends on it.
func TestRuleOrderPreserved(t *testing.T) {
	e := loadWithGlobal(t, `
[[agents.claude.route]]
match = "a*"
model = "first"

[[agents.claude.route]]
match = "b*"
model = "second"

[[agents.claude.route]]
match = "c*"
model = "third"
`)
	want := []string{"a*", "b*", "c*"}
	if len(e.Routes.V) != len(want) {
		t.Fatalf("got %d rules, want %d", len(e.Routes.V), len(want))
	}
	for i, w := range want {
		if e.Routes.V[i].Match != w {
			t.Errorf("rule %d match = %q, want %q", i, e.Routes.V[i].Match, w)
		}
	}
}

// A rule an earlier rule already covers can never fire; warn, don't fail.
func TestShadowedRuleWarns(t *testing.T) {
	e := loadWithGlobal(t, `
[[agents.claude.route]]
match = "*"
model = "catch-all"

[[agents.claude.route]]
match = "claude-opus-5"
model = "never-reached"
`)
	is := issueFor(e, "route[1]")
	if is == nil {
		t.Fatalf("want a shadowing warning, got %v", e.Check())
	}
	if is.Level != LevelWarning {
		t.Errorf("level = %v, want a warning", is.Level)
	}
	if !strings.Contains(is.Message, "can never match") {
		t.Errorf("message = %q", is.Message)
	}
}

func TestNonShadowedRulesAreQuiet(t *testing.T) {
	e := loadWithGlobal(t, `
[[agents.claude.route]]
match = "claude-opus-*"
model = "a"

[[agents.claude.route]]
match = "claude-sonnet-*"
model = "b"
`)
	assertNoErrors(t, e)
	for _, is := range e.Check() {
		if strings.Contains(is.Message, "can never match") {
			t.Errorf("unexpected shadowing warning: %s", is)
		}
	}
}

func TestRenameWithoutBackend(t *testing.T) {
	e := loadWithGlobal(t, `
[[agents.claude.route]]
match = "claude-opus-5"
model = "claude-sonnet-5"
`)
	rules, issues := e.Resolve("anthropic")
	if len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	if len(rules) != 1 || rules[0].Rerouted() {
		t.Fatalf("want one rename with no backend, got %+v", rules)
	}
	if rules[0].Model != "claude-sonnet-5" {
		t.Errorf("Model = %q", rules[0].Model)
	}
}

func TestUnknownBackendIsAnError(t *testing.T) {
	e := loadWithGlobal(t, `
[[agents.claude.route]]
match   = "claude-opus-5"
backend = "nosuch"
`)
	is := issueFor(e, "route[0]")
	if is == nil || is.Level != LevelError {
		t.Fatalf("want an error for the undeclared backend, got %v", e.Check())
	}
	if !strings.Contains(is.Message, "unknown backend") {
		t.Errorf("message = %q", is.Message)
	}
}

func TestBackendTypoSuggestion(t *testing.T) {
	e := loadWithGlobal(t, `
[backends.openrouter]
base_url    = "https://openrouter.ai/api"
api_key_env = "OPENROUTER_API_KEY"

[[agents.claude.route]]
match   = "claude-opus-5"
backend = "openrouterr"
`)
	is := issueFor(e, "route[0]")
	if is == nil || !strings.Contains(is.Message, "did you mean: openrouter") {
		t.Errorf("want a suggestion, got %v", is)
	}
}

// TestAPIStyleMismatchRejected is the routing-not-translation rule from
// internal-docs/04-interception.md: omni must never forward an Anthropic
// body to an OpenAI-shaped URL and hope.
func TestAPIStyleMismatchRejected(t *testing.T) {
	e := loadWithGlobal(t, `
[backends.local]
base_url    = "https://example.com/v1"
api_key_env = "OPENROUTER_API_KEY"
api_style   = "openai"

[[agents.claude.route]]
match   = "claude-opus-5"
backend = "local"
`)
	is := issueFor(e, "route[0]")
	if is == nil || is.Level != LevelError {
		t.Fatalf("want an error for the style mismatch, got %v", e.Check())
	}
	if !strings.Contains(is.Message, "does not translate") {
		t.Errorf("message = %q", is.Message)
	}
}

func TestBackendDefaultsToAnthropicStyle(t *testing.T) {
	e := loadWithGlobal(t, `
[backends.openrouter]
base_url    = "https://openrouter.ai/api"
api_key_env = "OPENROUTER_API_KEY"
`)
	b, ok := e.Backends.V["openrouter"]
	if !ok {
		t.Fatal("backend not loaded")
	}
	if b.APIStyle != "anthropic" {
		t.Errorf("APIStyle = %q, want the default anthropic", b.APIStyle)
	}
}

// TestPlaintextBackendRejected: a backend URL carries a live API key on
// every request, so http:// to a remote host is a credential leak.
func TestPlaintextBackendRejected(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeTestFile(t, filepath.Join(home, "omni.conf"), `
[backends.bad]
base_url    = "http://evil.example.com/api"
api_key_env = "OPENROUTER_API_KEY"
`)
	_, err := LoadFrom(home, "claude")
	if err == nil {
		t.Fatal("want LoadFrom to refuse a plaintext remote backend")
	}
	if !strings.Contains(err.Error(), "unencrypted") {
		t.Errorf("err = %v", err)
	}
}

func TestPlaintextLoopbackBackendAllowed(t *testing.T) {
	e := loadWithGlobal(t, `
[backends.local]
base_url    = "http://127.0.0.1:8080"
api_key_env = "OPENROUTER_API_KEY"
`)
	if _, ok := e.Backends.V["local"]; !ok {
		t.Errorf("a loopback http backend should be allowed: %v", e.Check())
	}
}

func TestBackendMissingFieldsRejected(t *testing.T) {
	cases := []struct{ name, conf, wantPath string }{
		{"no base_url", "[backends.b]\napi_key_env = \"K\"\n", "backends.b.base_url"},
		{"no api_key_env", "[backends.b]\nbase_url = \"https://x.example\"\n", "backends.b.api_key_env"},
		{"bad style", "[backends.b]\nbase_url = \"https://x.example\"\napi_key_env = \"K\"\napi_style = \"cohere\"\n", "backends.b.api_style"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := loadWithGlobal(t, tc.conf)
			if is := issueFor(e, tc.wantPath); is == nil || is.Level != LevelError {
				t.Errorf("want an error at %s, got %v", tc.wantPath, e.Check())
			}
		})
	}
}

func TestUnknownBackendKeyReported(t *testing.T) {
	e := loadWithGlobal(t, `
[backends.b]
base_url    = "https://x.example"
api_key_env = "K"
base_urls   = "typo"
`)
	if is := issueFor(e, "backends.b.base_urls"); is == nil {
		t.Errorf("want an unknown-key error, got %v", e.Check())
	}
}

// TestProjectConfigCannotRouteToBackend is the security boundary: a repo
// you cloned must not be able to redirect your prompts to a third party,
// even one you declared yourself.
func TestProjectConfigCannotRouteToBackend(t *testing.T) {
	home := testHome(t)
	cwd := testCWD(t)
	t.Setenv("OPENROUTER_API_KEY", "or-test-key")
	writeTestFile(t, filepath.Join(home, "omni.conf"), backendConf)
	writeTestFile(t, filepath.Join(cwd, ".omni.conf"), `
[[route]]
match   = "claude-opus-5"
backend = "openrouter"
`)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	// The global config's own rule survives; the project's must not appear.
	for _, r := range e.Routes.V {
		if r.Match == "claude-opus-5" {
			t.Errorf("project config's rule was applied: %+v", r)
		}
	}
	is := issueFor(e, "route")
	if is == nil || is.Level != LevelError {
		t.Fatalf("want an error, got %v", e.Check())
	}
	if !strings.Contains(is.Message, "may not route to a backend") {
		t.Errorf("message = %q", is.Message)
	}
}

// A project config may still do the harmless thing: rename a model within
// the same upstream.
func TestProjectConfigMayStillRename(t *testing.T) {
	home := testHome(t)
	cwd := testCWD(t)
	writeTestFile(t, filepath.Join(home, "omni.conf"), "[defaults]\nmode = \"record\"\n")
	writeTestFile(t, filepath.Join(cwd, ".omni.conf"), `
[[route]]
match = "claude-opus-5"
model = "claude-sonnet-5"
`)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if len(e.Routes.V) != 1 || e.Routes.V[0].Model != "claude-sonnet-5" {
		t.Errorf("rename not honored: %+v", e.Routes.V)
	}
	assertNoErrors(t, e)
}

// A project config must not be able to declare a backend at all.
func TestProjectConfigCannotDeclareBackend(t *testing.T) {
	home := testHome(t)
	cwd := testCWD(t)
	writeTestFile(t, filepath.Join(home, "omni.conf"), "[defaults]\nmode = \"record\"\n")
	writeTestFile(t, filepath.Join(cwd, ".omni.conf"), `
[backends.evil]
base_url    = "https://evil.example.com"
api_key_env = "OPENROUTER_API_KEY"
`)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if _, ok := e.Backends.V["evil"]; ok {
		t.Fatal("project config declared a backend; backends are global-only")
	}
}

// A credential pasted into api_key_env (instead of the variable's name) is
// the mistake worth catching loudly.
func TestCredentialInBackendConfigIsFatal(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	writeTestFile(t, filepath.Join(home, "omni.conf"), `
[backends.b]
base_url    = "https://x.example"
api_key_env = "sk-ant-api03-abcdefghijklmnop"
`)
	if _, err := LoadFrom(home, "claude"); err == nil {
		t.Fatal("want LoadFrom to refuse a credential in config")
	}
}

func TestMissingAPIKeyEnvWarns(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	t.Setenv("OPENROUTER_API_KEY", "")
	writeTestFile(t, filepath.Join(home, "omni.conf"), backendConf)
	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	is := issueFor(e, "backends.openrouter.api_key_env")
	if is == nil || is.Level != LevelWarning {
		t.Fatalf("want a warning about the unset variable, got %v", e.Check())
	}
}

// A rule with no match cannot be applied to anything.
func TestRuleWithoutMatchRejected(t *testing.T) {
	e := loadWithGlobal(t, `
[[agents.claude.route]]
model = "claude-sonnet-5"
`)
	if is := issueFor(e, "route[0]"); is == nil || is.Level != LevelError {
		t.Errorf("want an error for a rule with no match, got %v", e.Check())
	}
}

// A rule that names neither a backend nor a model is a no-op the user
// plainly did not intend.
func TestRuleWithoutEffectRejected(t *testing.T) {
	e := loadWithGlobal(t, `
[[agents.claude.route]]
match = "claude-*"
`)
	if is := issueFor(e, "route[0]"); is == nil || is.Level != LevelError {
		t.Errorf("want an error for a rule with no effect, got %v", e.Check())
	}
}

func TestUnknownRuleKeyReported(t *testing.T) {
	e := loadWithGlobal(t, `
[[agents.claude.route]]
match    = "claude-*"
backends = "openrouter"
`)
	if is := issueFor(e, "agents.claude.route[0].backends"); is == nil {
		t.Errorf("want an unknown-key error for a typo'd rule key, got %v", e.Check())
	}
}

// Backend credential policy is decided by host, not by name: a backend
// called "anthropic" pointed elsewhere must not talk omni into forwarding
// the agent's token there.
func TestAuthPolicy(t *testing.T) {
	const upstream = "https://api.anthropic.com"
	cases := []struct {
		name string
		b    Backend
		want AuthPolicy
	}{
		{"third party with key", Backend{BaseURL: "https://openrouter.ai/api", APIKeyEnv: "K"}, AuthSubstitute},
		{"same host, no key", Backend{BaseURL: "https://api.anthropic.com"}, AuthPreserve},
		{"local, no key", Backend{BaseURL: "http://127.0.0.1:11434"}, AuthNone},
		{"impostor named backend", Backend{BaseURL: "https://evil.example.com"}, AuthNone},
		{"same host but key given", Backend{BaseURL: "https://api.anthropic.com", APIKeyEnv: "K"}, AuthSubstitute},
	}
	for _, tc := range cases {
		if got := tc.b.Policy(upstream); got != tc.want {
			t.Errorf("%s: Policy = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A remote backend with no api_key_env would send unauthenticated requests.
func TestRemoteBackendWithoutKeyRejected(t *testing.T) {
	e := loadWithGlobal(t, `
[backends.b]
base_url = "https://example.com/api"
`)
	if is := issueFor(e, "backends.b.api_key_env"); is == nil || is.Level != LevelError {
		t.Errorf("want an error, got %v", e.Check())
	}
}

// A loopback backend with no key is the normal local-model case.
func TestLoopbackBackendWithoutKeyAllowed(t *testing.T) {
	e := loadWithGlobal(t, `
[backends.local]
base_url = "http://127.0.0.1:11434"
`)
	assertNoErrors(t, e)
}

// TestDerivedIssuesNotDuplicatedByOverride: Override re-runs the derived
// checks on the same *Effective, and a check that appended rather than
// rebuilt would report the same problem once per call. `omni --mode record
// claude` printed the unset-key warning twice before this was fixed.
func TestDerivedIssuesNotDuplicatedByOverride(t *testing.T) {
	home := testHome(t)
	testCWD(t)
	t.Setenv("OPENROUTER_API_KEY", "")
	writeTestFile(t, filepath.Join(home, "omni.conf"), backendConf)

	e, err := LoadFrom(home, "claude")
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	before := countIssues(e, "backends.openrouter.api_key_env")
	if before != 1 {
		t.Fatalf("got %d warnings after Load, want 1", before)
	}
	if err := e.Override(map[string]string{"mode": "record"}, "command line"); err != nil {
		t.Fatalf("Override: %v", err)
	}
	if after := countIssues(e, "backends.openrouter.api_key_env"); after != 1 {
		t.Errorf("got %d warnings after Override, want 1", after)
	}
}

func countIssues(e *Effective, path string) int {
	n := 0
	for _, is := range e.Check() {
		if is.Path == path {
			n++
		}
	}
	return n
}

// TestBaseURLTrailingV1Warns is the regression this check exists for. Every
// provider documents its own base URL with the /v1 included, so that is what
// gets pasted into base_url -- and omni then joins the agent's /v1/messages
// onto it and requests /api/v1/v1/messages. The 404 that comes back is an
// HTML page from the provider's website, which the agent reports as a model
// problem, so nothing in the visible failure points at base_url.
func TestBaseURLTrailingV1Warns(t *testing.T) {
	e := loadWithGlobal(t, `
[backends.openrouter]
base_url    = "https://openrouter.ai/api/v1"
api_key_env = "OPENROUTER_API_KEY"
`)
	is := issueFor(e, "backends.openrouter.base_url")
	if is == nil {
		t.Fatal("no issue for a base_url ending in /v1")
	}
	if is.Level != LevelWarning {
		t.Errorf("Level = %v, want LevelWarning (it must not block a launch)", is.Level)
	}
	// The message has to carry both halves: what would actually be requested,
	// and the base_url that fixes it.
	if !strings.Contains(is.Message, "https://openrouter.ai/api/v1/v1/messages") {
		t.Errorf("message does not show the doubled URL: %q", is.Message)
	}
	if !strings.Contains(is.Message, `"https://openrouter.ai/api"`) {
		t.Errorf("message does not show the corrected base_url: %q", is.Message)
	}
	// A warning, so the backend still loads and stays usable.
	if b, ok := e.Backends.V["openrouter"]; !ok {
		t.Error("backend was dropped by a warning")
	} else if b.BaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("BaseURL = %q, want the URL as written (omni must not rewrite it)", b.BaseURL)
	}
}

// TestBaseURLFullEndpointWarns covers the other paste: the whole endpoint,
// not just the version prefix.
func TestBaseURLFullEndpointWarns(t *testing.T) {
	e := loadWithGlobal(t, `
[backends.openrouter]
base_url    = "https://openrouter.ai/api/v1/messages"
api_key_env = "OPENROUTER_API_KEY"
`)
	is := issueFor(e, "backends.openrouter.base_url")
	if is == nil {
		t.Fatal("no issue for a base_url ending in /v1/messages")
	}
	if !strings.Contains(is.Message, `"https://openrouter.ai/api"`) {
		t.Errorf("fix should strip the whole endpoint, got: %q", is.Message)
	}
}

// TestBaseURLCorrectIsSilent is the false-positive guard. The check fires on
// a path *tail*, so a host or path that merely contains "v1" somewhere must
// stay quiet -- a warning on a working config trains people to ignore them.
func TestBaseURLCorrectIsSilent(t *testing.T) {
	for _, base := range []string{
		"https://openrouter.ai/api",
		"https://api.anthropic.com",
		"https://example.com/v1beta",
		"https://example.com/myv1",
		"https://v1.example.com",
		"http://127.0.0.1:11434",
	} {
		t.Run(base, func(t *testing.T) {
			e := loadWithGlobal(t, `
[backends.b]
base_url    = "`+base+`"
api_key_env = "OPENROUTER_API_KEY"
`)
			if is := issueFor(e, "backends.b.base_url"); is != nil {
				t.Errorf("unexpected issue for a correct base_url: %s", is.Message)
			}
		})
	}
}
