package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// failingUpstream is a stand-in backend that answers every request with a
// fixed status, headers and body — the shape of a provider rejecting a
// credential.
func failingUpstream(t *testing.T, status int, hdr map[string]string, body string) *url.URL {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing upstream URL: %v", err)
	}
	return u
}

// failRecorder collects RouteFailure values from the proxy's goroutine.
type failRecorder struct {
	mu   sync.Mutex
	seen []RouteFailure
}

func (fr *failRecorder) record(f RouteFailure) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	fr.seen = append(fr.seen, f)
}

func (fr *failRecorder) all() []RouteFailure {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	return append([]RouteFailure(nil), fr.seen...)
}

// startFailProxy starts a proxy whose default upstream is def, routing by
// rules, reporting failures into fr. A nil fr installs no callback at all,
// which is what a run without --verbose looks like.
func startFailProxy(t *testing.T, def *url.URL, rules []Rule, fr *failRecorder) *Server {
	t.Helper()
	rt := NewRouter(rules)
	if rt != nil && fr != nil {
		rt.OnRouteFailure = fr.record
	}
	s, err := New(Config{
		Upstream:        def,
		ExtraMiddleware: []RawMiddleware{RoutingMiddleware(rt)},
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

const haikuRequest = `{"model":"claude-haiku-4-5","max_tokens":1}`

// openrouterRule routes haiku to a backend at u, the shape that produced the
// original opaque failure.
func openrouterRule(u *url.URL, key string) []Rule {
	return []Rule{{
		Match: "claude-haiku-4-5*",
		Model: "minimax/minimax-m3:free",
		Backend: &Backend{
			Name:   "openrouter",
			URL:    u,
			APIKey: key,
		},
	}}
}

// TestRoutedFailureIsReported is the whole point of the feature: a dead key
// on a routed backend must produce one omni-side line naming the backend,
// both models, the status, and what the backend actually said. Without it the
// user sees only their agent's retry counter.
func TestRoutedFailureIsReported(t *testing.T) {
	body := `{"error":{"message":"User not found.","code":401}}`
	backend := failingUpstream(t, http.StatusUnauthorized,
		map[string]string{"Content-Type": "application/json"}, body)
	def := failingUpstream(t, http.StatusOK, nil, `{"ok":true}`)

	fr := &failRecorder{}
	s := startFailProxy(t, def, openrouterRule(backend, "sk-or-v1-dead"), fr)

	resp := postMessages(t, s, haikuRequest)
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if string(got) != body {
		t.Errorf("body was not forwarded intact:\n got: %s\nwant: %s", got, body)
	}

	seen := fr.all()
	if len(seen) != 1 {
		t.Fatalf("got %d failures, want exactly 1: %+v", len(seen), seen)
	}
	f := seen[0]
	if f.Status != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", f.Status)
	}
	if f.Backend != "openrouter" {
		t.Errorf("Backend = %q, want %q", f.Backend, "openrouter")
	}
	if f.From != "claude-haiku-4-5" {
		t.Errorf("From = %q, want the model the agent asked for", f.From)
	}
	if f.To != "minimax/minimax-m3:free" {
		t.Errorf("To = %q, want the model actually sent", f.To)
	}
	if !strings.Contains(f.Body, "User not found.") {
		t.Errorf("excerpt does not quote the backend's message: %q", f.Body)
	}
}

// TestUnroutedFailureIsSilent is the regression that matters most. omni is
// only entitled to comment on requests it redirected; a plain record-only
// session forwarding a 401 from the agent's own provider must be as silent as
// it was before this feature existed, and byte-identical besides.
func TestUnroutedFailureIsSilent(t *testing.T) {
	body := `{"type":"error","error":{"type":"authentication_error"}}`
	def := failingUpstream(t, http.StatusUnauthorized,
		map[string]string{"Content-Type": "application/json"}, body)

	fr := &failRecorder{}
	// A rule that cannot match the request being sent.
	s := startFailProxy(t, def, []Rule{{Match: "gpt-*", Model: "other"}}, fr)

	resp := postMessages(t, s, haikuRequest)
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(got) != body {
		t.Errorf("body mismatch:\n got: %s\nwant: %s", got, body)
	}
	if n := len(fr.all()); n != 0 {
		t.Errorf("reported %d failures for a request no rule claimed; want 0", n)
	}
}

// TestRoutedSuccessIsSilent pins the guard that keeps this feature away from
// the streaming path: a response below 400 is never inspected, so a healthy
// generation costs nothing.
func TestRoutedSuccessIsSilent(t *testing.T) {
	backend := failingUpstream(t, http.StatusOK,
		map[string]string{"Content-Type": "application/json"}, `{"ok":true}`)
	def := failingUpstream(t, http.StatusOK, nil, `{}`)

	fr := &failRecorder{}
	s := startFailProxy(t, def, openrouterRule(backend, "sk-or-v1-live"), fr)

	resp := postMessages(t, s, haikuRequest)
	io.Copy(io.Discard, resp.Body)

	if n := len(fr.all()); n != 0 {
		t.Errorf("reported %d failures for a 200; want 0", n)
	}
}

// TestNoCallbackLeavesResponseUntouched covers a run without --verbose: with
// nothing listening, the body must not even be read, let alone re-wrapped.
func TestNoCallbackLeavesResponseUntouched(t *testing.T) {
	body := `{"error":{"message":"User not found.","code":401}}`
	backend := failingUpstream(t, http.StatusUnauthorized,
		map[string]string{"Content-Type": "application/json"}, body)
	def := failingUpstream(t, http.StatusOK, nil, `{}`)

	s := startFailProxy(t, def, openrouterRule(backend, "sk-or-v1-dead"), nil)

	resp := postMessages(t, s, haikuRequest)
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if string(got) != body {
		t.Errorf("body mismatch:\n got: %s\nwant: %s", got, body)
	}
}

// TestStreamingErrorReportsStatusWithoutExcerpt: an error delivered as SSE is
// still worth naming, but not worth reading into. Doc 05 §2 — never buffer a
// stream.
func TestStreamingErrorReportsStatusWithoutExcerpt(t *testing.T) {
	backend := failingUpstream(t, http.StatusTooManyRequests,
		map[string]string{"Content-Type": "text/event-stream"},
		"event: error\ndata: {\"message\":\"slow down\"}\n\n")
	def := failingUpstream(t, http.StatusOK, nil, `{}`)

	fr := &failRecorder{}
	s := startFailProxy(t, def, openrouterRule(backend, "k"), fr)

	resp := postMessages(t, s, haikuRequest)
	got, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(got), "slow down") {
		t.Errorf("stream body not forwarded: %q", got)
	}

	seen := fr.all()
	if len(seen) != 1 {
		t.Fatalf("got %d failures, want 1", len(seen))
	}
	if seen[0].Status != http.StatusTooManyRequests {
		t.Errorf("Status = %d, want 429", seen[0].Status)
	}
	if seen[0].Body != "" {
		t.Errorf("excerpted a stream body: %q", seen[0].Body)
	}
}

// TestEncodedErrorReportsStatusWithoutExcerpt: agents send their own
// Accept-Encoding, so net/http hands us the compressed bytes. Quoting them
// would print binary noise.
func TestEncodedErrorReportsStatusWithoutExcerpt(t *testing.T) {
	backend := failingUpstream(t, http.StatusBadRequest, map[string]string{
		"Content-Type":     "application/json",
		"Content-Encoding": "gzip",
	}, "\x1f\x8b\x08\x00garbage")
	def := failingUpstream(t, http.StatusOK, nil, `{}`)

	fr := &failRecorder{}
	s := startFailProxy(t, def, openrouterRule(backend, "k"), fr)

	resp := postMessages(t, s, haikuRequest)
	io.Copy(io.Discard, resp.Body)

	seen := fr.all()
	if len(seen) != 1 {
		t.Fatalf("got %d failures, want 1", len(seen))
	}
	if seen[0].Status != http.StatusBadRequest {
		t.Errorf("Status = %d, want 400", seen[0].Status)
	}
	if seen[0].Body != "" {
		t.Errorf("excerpted a content-encoded body: %q", seen[0].Body)
	}
}

// TestExcerptRedactsBackendKey: some providers echo the presented credential
// back in the error. The excerpt goes to stderr, so it must not carry one.
func TestExcerptRedactsBackendKey(t *testing.T) {
	const key = "sk-or-v1-3c1f9a2b7e4d6081"
	backend := failingUpstream(t, http.StatusUnauthorized,
		map[string]string{"Content-Type": "application/json"},
		`{"error":{"message":"no such key: `+key+`"}}`)
	def := failingUpstream(t, http.StatusOK, nil, `{}`)

	fr := &failRecorder{}
	s := startFailProxy(t, def, openrouterRule(backend, key), fr)

	resp := postMessages(t, s, haikuRequest)
	io.Copy(io.Discard, resp.Body)

	seen := fr.all()
	if len(seen) != 1 {
		t.Fatalf("got %d failures, want 1", len(seen))
	}
	if strings.Contains(seen[0].Body, key) {
		t.Errorf("excerpt leaked the backend credential: %q", seen[0].Body)
	}
	if !strings.Contains(seen[0].Body, "[redacted]") {
		t.Errorf("excerpt does not mark the redaction: %q", seen[0].Body)
	}
}

// TestExcerptIsSingleLineAndBounded: the excerpt lands on the stderr of a
// full-screen TUI, so it must not carry escape sequences or run long.
func TestExcerptIsSingleLineAndBounded(t *testing.T) {
	long := `{"error":{"message":"` + "\x1b[2J\x1b[H" + strings.Repeat("boom ", 400) + `"}}`
	backend := failingUpstream(t, http.StatusInternalServerError,
		map[string]string{"Content-Type": "application/json"}, long)
	def := failingUpstream(t, http.StatusOK, nil, `{}`)

	fr := &failRecorder{}
	s := startFailProxy(t, def, openrouterRule(backend, "k"), fr)

	resp := postMessages(t, s, haikuRequest)
	got, _ := io.ReadAll(resp.Body)
	if string(got) != long {
		t.Error("body was not forwarded intact after the excerpt was taken")
	}

	seen := fr.all()
	if len(seen) != 1 {
		t.Fatalf("got %d failures, want 1", len(seen))
	}
	ex := seen[0].Body
	if strings.ContainsAny(ex, "\x1b\n\r\t") {
		t.Errorf("excerpt carries control characters: %q", ex)
	}
	// errorExcerptMax bytes plus the one-rune ellipsis.
	if len(ex) > errorExcerptMax+4 {
		t.Errorf("excerpt is %d bytes, want <= %d", len(ex), errorExcerptMax+4)
	}
	if !strings.HasSuffix(ex, "…") {
		t.Errorf("truncated excerpt is not marked: %q", ex)
	}
}

// TestRenameOnlyFailureIsReported: a rule that changes the model but keeps
// the agent's own upstream is still a decision omni made, so a failure that
// follows it is still attributable.
func TestRenameOnlyFailureIsReported(t *testing.T) {
	def := failingUpstream(t, http.StatusNotFound,
		map[string]string{"Content-Type": "application/json"},
		`{"error":{"message":"model not found"}}`)

	fr := &failRecorder{}
	s := startFailProxy(t, def, []Rule{{
		Match: "claude-haiku-4-5*",
		Model: "claude-sonnet-5",
	}}, fr)

	resp := postMessages(t, s, haikuRequest)
	io.Copy(io.Discard, resp.Body)

	seen := fr.all()
	if len(seen) != 1 {
		t.Fatalf("got %d failures, want 1", len(seen))
	}
	if seen[0].Backend != "" {
		t.Errorf("Backend = %q, want empty for a default-upstream rule", seen[0].Backend)
	}
	if seen[0].To != "claude-sonnet-5" {
		t.Errorf("To = %q, want the renamed model", seen[0].To)
	}
}
