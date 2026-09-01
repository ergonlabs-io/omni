package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// capture is a stand-in upstream that records exactly what reached it.
type capture struct {
	srv    *httptest.Server
	path   string
	body   []byte
	header http.Header
}

func newCapture(t *testing.T) *capture {
	t.Helper()
	c := &capture{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.path, c.body, c.header = r.URL.Path, b, r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *capture) url(t *testing.T) *url.URL {
	t.Helper()
	u, err := url.Parse(c.srv.URL)
	if err != nil {
		t.Fatalf("parsing capture URL: %v", err)
	}
	return u
}

// startRouted starts a proxy whose default upstream is def, with routing.
func startRouted(t *testing.T, def *capture, rules []Rule) *Server {
	t.Helper()
	s, err := New(Config{
		Upstream:        def.url(t),
		ExtraMiddleware: []RawMiddleware{RoutingMiddleware(NewRouter(rules))},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})
	return s
}

// postMessages sends body to the proxy's /v1/messages with the header set
// Claude Code actually sends (see a recorded session).
func postMessages(t *testing.T, s *Server, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, s.URL()+"/v1/messages", strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-ant-oat01-REAL-ANTHROPIC-TOKEN")
	req.Header.Set("X-Api-Key", "sk-ant-api03-ALSO-REAL")
	req.Header.Set("Anthropic-Beta", "claude-code-20250219,oauth-2025-04-20")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func bodyModel(t *testing.T, b []byte) string {
	t.Helper()
	var m struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshalling captured body: %v\n%s", err, b)
	}
	return m.Model
}

// TestRouteToBackendStripsAnthropicCredential is the security test for this
// feature. A route that sends traffic to a third party must never carry the
// agent's Anthropic credential with it.
func TestRouteToBackendStripsAnthropicCredential(t *testing.T) {
	def, backend := newCapture(t), newCapture(t)
	s := startRouted(t, def, []Rule{{
		Match: "claude-haiku-4-5*",
		Model: "minimax/minimax-m3:free",
		Backend: &Backend{
			Name:    "openrouter",
			URL:     backend.url(t),
			APIKey:  "or-test-key",
			Headers: map[string]string{"X-Title": "omni"},
		},
	}})

	postMessages(t, s, `{"model":"claude-haiku-4-5-20251001","max_tokens":16}`)

	if def.body != nil {
		t.Fatal("routed request reached the default upstream; it must go to the backend only")
	}
	if got := backend.header.Get("Authorization"); got != "Bearer or-test-key" {
		t.Errorf("Authorization = %q, want the backend's own key", got)
	}
	if got := backend.header.Get("X-Api-Key"); got != "" {
		t.Errorf("X-Api-Key leaked to backend: %q", got)
	}
	// Non-credential headers are forwarded, not eaten: omni does not decide
	// on the agent's behalf which headers a backend wants.
	if got := backend.header.Get("Anthropic-Beta"); got != "claude-code-20250219,oauth-2025-04-20" {
		t.Errorf("Anthropic-Beta = %q, want it forwarded verbatim", got)
	}
	if got := backend.header.Get("Anthropic-Version"); got != "2023-06-01" {
		t.Errorf("Anthropic-Version = %q, want it forwarded verbatim", got)
	}
	// Belt and braces: no Anthropic credential in any header value.
	for k, vs := range backend.header {
		for _, v := range vs {
			if strings.Contains(v, "sk-ant-") {
				t.Errorf("Anthropic credential leaked in header %s: %q", k, v)
			}
		}
	}
	if got := backend.header.Get("X-Title"); got != "omni" {
		t.Errorf("backend header X-Title = %q, want omni", got)
	}
	if got := bodyModel(t, backend.body); got != "minimax/minimax-m3:free" {
		t.Errorf("model = %q, want the rewritten name", got)
	}
	if backend.path != "/v1/messages" {
		t.Errorf("backend path = %q, want /v1/messages", backend.path)
	}
}

// TestUnroutedRequestIsUntouched guards the promise that turning routing on
// costs nothing for models no route mentions.
func TestUnroutedRequestIsUntouched(t *testing.T) {
	def, backend := newCapture(t), newCapture(t)
	s := startRouted(t, def, []Rule{{
		Match:   "claude-haiku-4-5*",
		Model:   "minimax/minimax-m3:free",
		Backend: &Backend{Name: "openrouter", URL: backend.url(t), APIKey: "or-test-key"},
	}})

	const body = `{"model":"claude-opus-5","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`
	postMessages(t, s, body)

	if backend.body != nil {
		t.Fatal("unrouted request reached the backend")
	}
	if string(def.body) != body {
		t.Errorf("body was modified:\n got %s\nwant %s", def.body, body)
	}
	if got := def.header.Get("Authorization"); got != "Bearer sk-ant-oat01-REAL-ANTHROPIC-TOKEN" {
		t.Errorf("Authorization to default upstream = %q, want it forwarded verbatim", got)
	}
	if got := def.header.Get("Anthropic-Beta"); got != "claude-code-20250219,oauth-2025-04-20" {
		t.Errorf("Anthropic-Beta = %q, want it forwarded verbatim", got)
	}
}

// TestRenameKeepsDefaultUpstream covers a model_map entry with no backend:
// the name changes, the destination does not, and the Anthropic credential
// must still be forwarded because the request is still going to Anthropic.
func TestRenameKeepsDefaultUpstream(t *testing.T) {
	def := newCapture(t)
	s := startRouted(t, def, []Rule{{Match: "claude-opus-5", Model: "claude-sonnet-5"}})

	postMessages(t, s, `{"model":"claude-opus-5","max_tokens":16}`)

	if got := bodyModel(t, def.body); got != "claude-sonnet-5" {
		t.Errorf("model = %q, want claude-sonnet-5", got)
	}
	if got := def.header.Get("Authorization"); got != "Bearer sk-ant-oat01-REAL-ANTHROPIC-TOKEN" {
		t.Errorf("Authorization = %q, want it preserved for a same-upstream rename", got)
	}
}

// TestContentLengthMatchesRewrittenBody: the substituted model name is a
// different length, so a stale Content-Length would truncate the request or
// hang the upstream.
func TestContentLengthMatchesRewrittenBody(t *testing.T) {
	def, backend := newCapture(t), newCapture(t)
	s := startRouted(t, def, []Rule{{
		Match:   "m",
		Model:   "a-considerably-longer-model-identifier",
		Backend: &Backend{Name: "b", URL: backend.url(t), APIKey: "k"},
	}})

	postMessages(t, s, `{"model":"m","max_tokens":16}`)

	want := `{"model":"a-considerably-longer-model-identifier","max_tokens":16}`
	if string(backend.body) != want {
		t.Errorf("body =\n %s\nwant %s", backend.body, want)
	}
	if got := backend.header.Get("Content-Length"); got != "" && got != strconv.Itoa(len(want)) {
		t.Errorf("Content-Length = %s, want %d", got, len(want))
	}
}

// TestNonMessagesPathIsNotInspected: routing must not touch endpoints omni
// does not model, even when a body happens to carry a matching model name.
func TestNonMessagesPathIsNotInspected(t *testing.T) {
	def, backend := newCapture(t), newCapture(t)
	s := startRouted(t, def, []Rule{{
		Match:   "claude-opus-5",
		Model:   "other",
		Backend: &Backend{Name: "b", URL: backend.url(t), APIKey: "k"},
	}})

	const body = `{"model":"claude-opus-5"}`
	req, _ := http.NewRequest(http.MethodPost, s.URL()+"/v1/complete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if backend.body != nil {
		t.Fatal("/v1/complete was rerouted; only Messages endpoints are routable")
	}
	if string(def.body) != body {
		t.Errorf("body = %s, want it untouched", def.body)
	}
}

// TestMalformedBodyForwardsUnrouted: an unparsable body must not become an
// outage. Fail open, forward, warn.
func TestMalformedBodyForwardsUnrouted(t *testing.T) {
	def, backend := newCapture(t), newCapture(t)
	s := startRouted(t, def, []Rule{{
		Match:   "claude-opus-5",
		Model:   "other",
		Backend: &Backend{Name: "b", URL: backend.url(t), APIKey: "k"},
	}})

	const body = `{"model":"claude-opus-5"` // truncated
	postMessages(t, s, body)

	if backend.body != nil {
		t.Fatal("malformed body was routed")
	}
	if string(def.body) != body {
		t.Errorf("body = %s, want it forwarded verbatim", def.body)
	}
}

// TestNilRouterIsNoOp: RoutingMiddleware(nil) must be a pure passthrough so
// record-only sessions pay nothing for the feature existing.
func TestNilRouterIsNoOp(t *testing.T) {
	def := newCapture(t)
	s, err := New(Config{
		Upstream:        def.url(t),
		ExtraMiddleware: []RawMiddleware{RoutingMiddleware(nil)},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})

	const body = `{"model":"claude-opus-5"}`
	postMessages(t, s, body)
	if string(def.body) != body {
		t.Errorf("body = %s, want %s", def.body, body)
	}
}

func TestNewRouterEmptyIsNil(t *testing.T) {
	if NewRouter(nil) != nil {
		t.Error("NewRouter(nil) must return nil so callers need no special case")
	}
}

// TestFirstMatchWins: rules are ordered and the first matching one is the
// one that fires, even when a later rule is a tighter fit. Order is part of
// the meaning, so this must not depend on map iteration or specificity.
func TestFirstMatchWins(t *testing.T) {
	def, a, b := newCapture(t), newCapture(t), newCapture(t)
	s := startRouted(t, def, []Rule{
		{Match: "claude-*", Model: "broad", Backend: &Backend{Name: "a", URL: a.url(t), APIKey: "k"}},
		{Match: "claude-opus-5", Model: "narrow", Backend: &Backend{Name: "b", URL: b.url(t), APIKey: "k"}},
	})

	postMessages(t, s, `{"model":"claude-opus-5","max_tokens":16}`)

	if b.body != nil {
		t.Fatal("the later, narrower rule fired; first match must win")
	}
	if got := bodyModel(t, a.body); got != "broad" {
		t.Errorf("model = %q, want the first rule's target", got)
	}
}

// TestGlobMatchesBothHaikuForms is the case this feature was built for: one
// rule covering the bare alias and the dated id.
func TestGlobMatchesBothHaikuForms(t *testing.T) {
	for _, model := range []string{"claude-haiku-4-5", "claude-haiku-4-5-20251001"} {
		t.Run(model, func(t *testing.T) {
			def, backend := newCapture(t), newCapture(t)
			s := startRouted(t, def, []Rule{{
				Match:   "claude-haiku-4-5*",
				Model:   "minimax/minimax-m3:free",
				Backend: &Backend{Name: "openrouter", URL: backend.url(t), APIKey: "k"},
			}})

			postMessages(t, s, `{"model":"`+model+`","max_tokens":16}`)

			if backend.body == nil {
				t.Fatalf("%s did not match claude-haiku-4-5*", model)
			}
			if got := bodyModel(t, backend.body); got != "minimax/minimax-m3:free" {
				t.Errorf("model = %q", got)
			}
		})
	}
}

// TestBackendWithoutModelKeepsRequestedModel: a rule that only changes the
// destination must leave the body's model alone, byte for byte.
func TestBackendWithoutModelKeepsRequestedModel(t *testing.T) {
	def, backend := newCapture(t), newCapture(t)
	s := startRouted(t, def, []Rule{{
		Match:   "claude-*",
		Backend: &Backend{Name: "mirror", URL: backend.url(t), APIKey: "k"},
	}})

	const body = `{"model":"claude-opus-5","max_tokens":16}`
	postMessages(t, s, body)

	if string(backend.body) != body {
		t.Errorf("body = %s, want it unchanged", backend.body)
	}
	if got := backend.header.Get("Authorization"); got != "Bearer k" {
		t.Errorf("Authorization = %q, want the backend's key", got)
	}
}

// TestPreserveAuthForwardsCredentials: a backend resolving to the agent's
// own upstream crosses no trust boundary, so its credentials and provider
// headers must go through untouched.
func TestPreserveAuthForwardsCredentials(t *testing.T) {
	def, same := newCapture(t), newCapture(t)
	s := startRouted(t, def, []Rule{{
		Match:   "claude-opus-5",
		Model:   "claude-sonnet-5",
		Backend: &Backend{Name: "anthropic", URL: same.url(t), PreserveAuth: true},
	}})

	postMessages(t, s, `{"model":"claude-opus-5","max_tokens":16}`)

	if got := same.header.Get("Authorization"); got != "Bearer sk-ant-oat01-REAL-ANTHROPIC-TOKEN" {
		t.Errorf("Authorization = %q, want it preserved", got)
	}
	if got := same.header.Get("Anthropic-Beta"); got == "" {
		t.Error("Anthropic-Beta was stripped for a same-host backend")
	}
	if got := bodyModel(t, same.body); got != "claude-sonnet-5" {
		t.Errorf("model = %q, want the rewrite to still happen", got)
	}
}

// TestNoAPIKeySendsNoCredential: a local endpoint wanting no auth still
// gets the agent's credential stripped.
func TestNoAPIKeySendsNoCredential(t *testing.T) {
	def, local := newCapture(t), newCapture(t)
	s := startRouted(t, def, []Rule{{
		Match:   "claude-opus-5",
		Model:   "qwen",
		Backend: &Backend{Name: "local", URL: local.url(t)},
	}})

	postMessages(t, s, `{"model":"claude-opus-5","max_tokens":16}`)

	if got := local.header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want none", got)
	}
	if got := local.header.Get("X-Api-Key"); got != "" {
		t.Errorf("X-Api-Key leaked to a local backend: %q", got)
	}
}

// TestOnlyCredentialHeadersAreRemoved pins the boundary exactly: every
// header Claude Code actually sends (taken from a recorded session) must
// arrive at the backend unchanged, except the ones carrying a credential.
func TestOnlyCredentialHeadersAreRemoved(t *testing.T) {
	def, backend := newCapture(t), newCapture(t)
	s := startRouted(t, def, []Rule{{
		Match:   "m",
		Model:   "target",
		Backend: &Backend{Name: "b", URL: backend.url(t), APIKey: "or-key"},
	}})

	// Verbatim from ~/.omni/sessions/.../request.headers.json, plus the
	// api-key variants an ANTHROPIC_API_KEY session would use instead.
	forwarded := map[string]string{
		"Anthropic-Version":                         "2023-06-01",
		"Anthropic-Beta":                            "claude-code-20250219,oauth-2025-04-20",
		"Anthropic-Dangerous-Direct-Browser-Access": "true",
		"X-App":                       "cli",
		"X-Claude-Code-Session-Id":    "f04cb492-8f7f-40c9-b514-9a0af775f0a1",
		"X-Stainless-Arch":            "arm64",
		"X-Stainless-Lang":            "js",
		"X-Stainless-Os":              "MacOS",
		"X-Stainless-Package-Version": "0.112.1",
		"X-Stainless-Retry-Count":     "0",
		"X-Stainless-Runtime":         "node",
		"X-Stainless-Runtime-Version": "v26.3.0",
		"X-Stainless-Timeout":         "70",
		"User-Agent":                  "claude-cli/2.1.251 (external, cli)",
	}
	stripped := map[string]string{
		"Authorization":       "Bearer sk-ant-oat01-REAL",
		"X-Api-Key":           "sk-ant-api03-REAL",
		"Anthropic-Api-Key":   "sk-ant-api03-REAL",
		"Some-Vendor-Api-Key": "vendor-secret",
	}

	req, err := http.NewRequest(http.MethodPost, s.URL()+"/v1/messages",
		strings.NewReader(`{"model":"m","max_tokens":16}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range forwarded {
		req.Header.Set(k, v)
	}
	for k, v := range stripped {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	for k, want := range forwarded {
		if got := backend.header.Get(k); got != want {
			t.Errorf("%s = %q, want it forwarded verbatim (%q)", k, got, want)
		}
	}
	for k := range stripped {
		if k == "Authorization" {
			continue // replaced, not merely removed
		}
		if got := backend.header.Get(k); got != "" {
			t.Errorf("credential header %s reached the backend: %q", k, got)
		}
	}
	if got := backend.header.Get("Authorization"); got != "Bearer or-key" {
		t.Errorf("Authorization = %q, want the backend's own key", got)
	}
}
