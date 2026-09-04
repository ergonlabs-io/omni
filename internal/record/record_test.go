package record

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestRecorder(t *testing.T, opts ...Option) *Recorder {
	t.Helper()
	dir := t.TempDir()
	r, err := New(filepath.Join(dir, "sessions"), "claude", "omni-test/0.0.0", opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

// writeStringSync drives a Write/Close of an io.WriteCloser and waits for the
// background sink goroutine to finish, so tests can make assertions
// immediately afterward without a race.
func writeAndClose(t *testing.T, w io.WriteCloser, chunks ...string) {
	t.Helper()
	for _, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// readEvents parses a session's exchanges.jsonl into its lines, in file
// order. A malformed line fails the test: the index is only useful if it is
// machine-readable, so "it wrote something" is not the assertion we want.
func readEvents(t *testing.T, dir string) []exchangeEvent {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, exchangeLogName))
	if err != nil {
		t.Fatalf("reading %s: %v", exchangeLogName, err)
	}
	var out []exchangeEvent
	for i, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var ev exchangeEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("%s line %d is not valid JSON (%v): %s", exchangeLogName, i+1, err, line)
		}
		out = append(out, ev)
	}
	return out
}

// eventFor returns the single event of the given type for exchange seq.
func eventFor(t *testing.T, dir, typ string, seq int) exchangeEvent {
	t.Helper()
	for _, ev := range readEvents(t, dir) {
		if ev.Type == typ && ev.Seq == seq {
			return ev
		}
	}
	t.Fatalf("no %q event for exchange %d in %s", typ, seq, exchangeLogName)
	return exchangeEvent{}
}

func TestNewCreatesSessionDir(t *testing.T) {
	r := newTestRecorder(t)
	info, err := os.Stat(r.Dir())
	if err != nil {
		t.Fatalf("session dir missing: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("session path is not a directory")
	}
	if !strings.HasSuffix(filepath.Base(r.Dir()), "-claude") {
		t.Errorf("session dir name %q does not end with agent name", r.Dir())
	}
}

func TestSessionDirCollisionGetsSuffixed(t *testing.T) {
	dir := t.TempDir()
	fixed := time.Date(2026, 8, 30, 8, 17, 2, 0, time.UTC)
	clk := func() time.Time { return fixed }

	r1, err := New(dir, "claude", "v0", withClock(clk))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r2, err := New(dir, "claude", "v0", withClock(clk))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r1.Dir() == r2.Dir() {
		t.Fatalf("two sessions starting at the same instant collided on directory %q", r1.Dir())
	}
}

func TestExchangeFileNaming(t *testing.T) {
	r := newTestRecorder(t)

	ex1 := r.Begin("POST", "/v1/messages", http.Header{"Content-Type": {"application/json"}}, []byte(`{"a":1}`))
	writeAndClose(t, ex1.ResponseWriter(200, http.Header{"Content-Type": {"application/json"}}), `{"usage":{"input_tokens":1}}`)

	ex2 := r.Begin("POST", "/v1/messages", http.Header{}, []byte(`{"a":2}`))
	writeAndClose(t, ex2.ResponseWriter(200, http.Header{"Content-Type": {"text/event-stream"}}), "data: {}\n\n")

	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Two files per exchange (verbatim bodies only), plus the two
	// session-level files. Header metadata lives in exchanges.jsonl, not in
	// a NNN.*.headers.json pair.
	wantFiles := []string{
		"001.request.json", "001.response.json",
		"002.request.json", "002.response.sse",
		exchangeLogName, "meta.json",
	}
	for _, f := range wantFiles {
		if _, err := os.Stat(filepath.Join(r.Dir(), f)); err != nil {
			t.Errorf("expected file %s: %v", f, err)
		}
	}

	entries, err := os.ReadDir(r.Dir())
	if err != nil {
		t.Fatalf("reading session dir: %v", err)
	}
	if len(entries) != len(wantFiles) {
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		t.Errorf("session dir has %d files, want %d: %v", len(entries), len(wantFiles), got)
	}
}

// TestExchangeIndex pins the shape of exchanges.jsonl: one request line and
// one response line per exchange, in order, each naming the sibling file
// that holds the verbatim body.
func TestExchangeIndex(t *testing.T) {
	r := newTestRecorder(t)

	ex1 := r.Begin("POST", "/v1/messages", http.Header{"Content-Type": {"application/json"}}, []byte(`{"a":1}`))
	writeAndClose(t, ex1.ResponseWriter(200, http.Header{"Content-Type": {"text/event-stream"}}), "data: {}\n\n")

	ex2 := r.Begin("POST", "/v1/messages?beta=true", http.Header{}, []byte(`{"a":2}`))
	writeAndClose(t, ex2.ResponseWriter(429, http.Header{"Content-Type": {"application/json"}}), `{"error":{}}`)

	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events := readEvents(t, r.Dir())
	if len(events) != 4 {
		t.Fatalf("got %d index lines, want 4 (2 exchanges x request+response)", len(events))
	}

	want := []struct {
		seq      int
		typ      string
		bodyFile string
	}{
		{1, "request", "001.request.json"},
		{1, "response", "001.response.sse"},
		{2, "request", "002.request.json"},
		{2, "response", "002.response.json"},
	}
	for i, w := range want {
		got := events[i]
		if got.Seq != w.seq || got.Type != w.typ {
			t.Errorf("line %d: seq/type = %d/%q, want %d/%q", i+1, got.Seq, got.Type, w.seq, w.typ)
		}
		if got.BodyFile != w.bodyFile {
			t.Errorf("line %d: body_file = %q, want %q", i+1, got.BodyFile, w.bodyFile)
		}
		if _, err := os.Stat(filepath.Join(r.Dir(), got.BodyFile)); err != nil {
			t.Errorf("line %d: body_file %q does not exist: %v", i+1, got.BodyFile, err)
		}
		if got.Time.IsZero() {
			t.Errorf("line %d: time not recorded", i+1)
		}
	}

	// Request-only and response-only fields must not bleed across types.
	if req := eventFor(t, r.Dir(), "request", 2); req.Method != "POST" || req.URL != "/v1/messages?beta=true" {
		t.Errorf("request event = %s %s, want POST /v1/messages?beta=true", req.Method, req.URL)
	} else if req.Status != 0 {
		t.Errorf("request event carries a status: %d", req.Status)
	}
	if resp := eventFor(t, r.Dir(), "response", 2); resp.Status != 429 {
		t.Errorf("response status = %d, want 429", resp.Status)
	} else if resp.Method != "" || resp.URL != "" {
		t.Errorf("response event carries request fields: %s %s", resp.Method, resp.URL)
	}
}

// TestExchangeIndexRecordsTTFB checks that the response line carries the
// latency the old per-exchange files never recorded anywhere.
func TestExchangeIndexRecordsTTFB(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 5, 21, 0, time.UTC)
	clk := func() time.Time { return now }

	dir := t.TempDir()
	r, err := New(dir, "claude", "v0", withClock(clk))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ex := r.Begin("POST", "/v1/messages", http.Header{}, nil)
	now = now.Add(1500 * time.Millisecond) // upstream took 1.5s to first byte
	writeAndClose(t, ex.ResponseWriter(200, http.Header{"Content-Type": {"application/json"}}), `{}`)
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := eventFor(t, r.Dir(), "response", 1).TTFBMs; got != 1500 {
		t.Errorf("ttfb_ms = %d, want 1500", got)
	}
}

// TestExchangeIndexIsAppendOnlyUnderConcurrency drives parallel in-flight
// exchanges (what a real agent's parallel tool calls produce) and asserts
// every line survived intact — an interleaved write would show up as a line
// that will not parse.
func TestExchangeIndexIsAppendOnlyUnderConcurrency(t *testing.T) {
	r := newTestRecorder(t)

	const n = 32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ex := r.Begin("POST", "/v1/messages", http.Header{"X-Api-Key": {"sk-ant-secret"}}, []byte(`{}`))
			w := ex.ResponseWriter(200, http.Header{"Content-Type": {"application/json"}})
			w.Write([]byte(`{}`))
			w.Close()
		}()
	}
	wg.Wait()
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events := readEvents(t, r.Dir()) // fails the test on any torn line
	if len(events) != 2*n {
		t.Fatalf("got %d index lines, want %d", len(events), 2*n)
	}
	seen := map[string]bool{}
	for _, ev := range events {
		key := fmt.Sprintf("%d/%s", ev.Seq, ev.Type)
		if seen[key] {
			t.Errorf("duplicate index line for %s", key)
		}
		seen[key] = true
	}
	for i := 1; i <= n; i++ {
		for _, typ := range []string{"request", "response"} {
			if !seen[fmt.Sprintf("%d/%s", i, typ)] {
				t.Errorf("missing %s line for exchange %d", typ, i)
			}
		}
	}
}

func TestRequestBodyWrittenVerbatim(t *testing.T) {
	r := newTestRecorder(t)
	body := []byte(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"hi"}]}`)
	r.Begin("POST", "/v1/messages", http.Header{}, body)
	r.Close()

	got, err := os.ReadFile(filepath.Join(r.Dir(), "001.request.json"))
	if err != nil {
		t.Fatalf("reading request file: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("request body not verbatim:\n got: %s\nwant: %s", got, body)
	}
}

func TestResponseBodyWrittenVerbatim(t *testing.T) {
	r := newTestRecorder(t)
	ex := r.Begin("POST", "/v1/messages", http.Header{}, nil)
	rw := ex.ResponseWriter(200, http.Header{"Content-Type": {"text/event-stream"}})
	frames := []string{"event: message_start\ndata: {\"type\":\"message_start\"}\n\n", "event: message_stop\ndata: {}\n\n"}
	writeAndClose(t, rw, frames...)
	r.Close()

	got, err := os.ReadFile(filepath.Join(r.Dir(), "001.response.sse"))
	if err != nil {
		t.Fatalf("reading response file: %v", err)
	}
	want := strings.Join(frames, "")
	if string(got) != want {
		t.Errorf("response body not verbatim:\n got: %q\nwant: %q", got, want)
	}
}

// TestCorpusHasNoCredentials is the most important test in this package: it
// asserts that credential material never survives into a recorded session,
// across every file type the recorder writes. See
// internal-docs/07-testing.md "Redaction is a test".
func TestCorpusHasNoCredentials(t *testing.T) {
	const (
		apiKey      = "sk-ant-api03-THIS_IS_A_FAKE_TEST_KEY_1234567890"
		bearerToken = "sk-ant-oat01-THIS_IS_A_FAKE_OAUTH_TOKEN_abcdefgh"
	)

	r := newTestRecorder(t)

	reqHeader := http.Header{
		"Authorization":      {"Bearer " + bearerToken},
		"X-Api-Key":          {apiKey},
		"Anthropic-Api-Key":  {apiKey}, // *-api-key suffix variant
		"Anthropic-Beta":     {"oauth-2025-04-20"},
		"Content-Type":       {"application/json"},
		"X-Totally-Fine-Key": {"not-a-credential-header"}, // must NOT be redacted (shape check)
	}
	body := []byte(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"hello, no secrets here"}]}`)

	ex := r.Begin("POST", "/v1/messages", reqHeader, body)

	respHeader := http.Header{"Content-Type": {"text/event-stream"}}
	rw := ex.ResponseWriter(200, respHeader)
	writeAndClose(t, rw,
		"event: message_start\n",
		`data: {"type":"message_start","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":50}}}`+"\n\n",
		"event: message_stop\ndata: {}\n\n",
	)

	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	entries, err := os.ReadDir(r.Dir())
	if err != nil {
		t.Fatalf("reading session dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no files written")
	}

	credentialShapes := []string{apiKey, bearerToken, "Bearer " + bearerToken}

	for _, e := range entries {
		path := filepath.Join(r.Dir(), e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		content := string(data)
		for _, secret := range credentialShapes {
			if strings.Contains(content, secret) {
				t.Errorf("file %s contains unredacted credential material: %q", e.Name(), secret)
			}
		}
	}

	// The header *keys* must still be visible (shape of auth used is
	// legitimate debugging info), only the values are secret.
	req := eventFor(t, r.Dir(), "request", 1)
	for _, key := range []string{"Authorization", "X-Api-Key", "Anthropic-Api-Key"} {
		vals, ok := req.Header[key]
		if !ok {
			t.Errorf("expected header key %q to survive redaction", key)
			continue
		}
		if len(vals) != 1 || vals[0] != redactedValue {
			t.Errorf("header %q = %v, want [%q]", key, vals, redactedValue)
		}
	}
	if got := req.Header["X-Totally-Fine-Key"]; len(got) != 1 || got[0] != "not-a-credential-header" {
		t.Errorf("non-credential header was mangled: %v", got)
	}
	if got := req.Header["Anthropic-Beta"]; len(got) != 1 || got[0] != "oauth-2025-04-20" {
		t.Errorf("non-credential header Anthropic-Beta was mangled: %v", got)
	}
}

func TestRedactionIsPositiveOptOut(t *testing.T) {
	const apiKey = "sk-ant-api03-OPTOUT_TEST_KEY_0000000000000000"

	r := newTestRecorder(t, WithRedaction(false))
	r.Begin("POST", "/v1/messages", http.Header{"X-Api-Key": {apiKey}}, nil)
	r.Close()

	data, err := os.ReadFile(filepath.Join(r.Dir(), exchangeLogName))
	if err != nil {
		t.Fatalf("reading %s: %v", exchangeLogName, err)
	}
	if !strings.Contains(string(data), apiKey) {
		t.Errorf("WithRedaction(false) should have left the credential value intact")
	}
}

func TestDefaultIsRedacted(t *testing.T) {
	const apiKey = "sk-ant-api03-DEFAULT_TEST_KEY_00000000000000"

	// No options at all: redaction must be on by default.
	dir := t.TempDir()
	r, err := New(dir, "claude", "v0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Begin("POST", "/v1/messages", http.Header{"X-Api-Key": {apiKey}}, nil)
	r.Close()

	data, err := os.ReadFile(filepath.Join(r.Dir(), exchangeLogName))
	if err != nil {
		t.Fatalf("reading %s: %v", exchangeLogName, err)
	}
	if strings.Contains(string(data), apiKey) {
		t.Fatalf("default construction (no options) leaked a credential — redaction must default on")
	}
}

func TestCacheReadTokensSurfacedInSummary(t *testing.T) {
	r := newTestRecorder(t)

	// Exchange 1: streaming, cache_read_input_tokens in message_start.
	ex1 := r.Begin("POST", "/v1/messages", http.Header{}, nil)
	rw1 := ex1.ResponseWriter(200, http.Header{"Content-Type": {"text/event-stream"}})
	writeAndClose(t, rw1,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":1000,"output_tokens":1}}}`+"\n\n",
		`data: {"type":"message_delta","delta":{},"usage":{"output_tokens":42}}`+"\n\n",
	)

	// Exchange 2: non-streaming JSON.
	ex2 := r.Begin("POST", "/v1/messages", http.Header{}, nil)
	rw2 := ex2.ResponseWriter(200, http.Header{"Content-Type": {"application/json"}})
	writeAndClose(t, rw2, `{"id":"msg_1","usage":{"input_tokens":20,"cache_read_input_tokens":2000,"output_tokens":7}}`)

	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(r.Dir(), "meta.json"))
	if err != nil {
		t.Fatalf("reading meta.json: %v", err)
	}
	var meta sessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("unmarshaling meta.json: %v", err)
	}

	if meta.Exchanges != 2 {
		t.Errorf("Exchanges = %d, want 2", meta.Exchanges)
	}
	if want := int64(1000 + 2000); meta.Summary.CacheReadInputTokensTotal != want {
		t.Errorf("CacheReadInputTokensTotal = %d, want %d", meta.Summary.CacheReadInputTokensTotal, want)
	}
	if want := int64(42 + 7); meta.Summary.OutputTokensTotal != want {
		t.Errorf("OutputTokensTotal = %d, want %d", meta.Summary.OutputTokensTotal, want)
	}
	if meta.EndTime == nil {
		t.Errorf("EndTime not set after Close")
	}
	if meta.Agent != "claude" {
		t.Errorf("Agent = %q, want claude", meta.Agent)
	}
	if meta.OmniVersion == "" {
		t.Errorf("OmniVersion not recorded")
	}
}

func TestUnparseableBodyIsSkippedNotFailed(t *testing.T) {
	r := newTestRecorder(t)
	ex := r.Begin("POST", "/v1/messages", http.Header{}, nil)
	rw := ex.ResponseWriter(500, http.Header{"Content-Type": {"application/json"}})
	// Not valid JSON at all — should be skipped silently, not panic/fail.
	writeAndClose(t, rw, "internal server error, not json")
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// If we got here without panicking, best-effort parsing degraded
	// correctly. Also confirm the raw bytes were still recorded verbatim.
	got, err := os.ReadFile(filepath.Join(r.Dir(), "001.response.json"))
	if err != nil {
		t.Fatalf("reading response file: %v", err)
	}
	if string(got) != "internal server error, not json" {
		t.Errorf("unparseable body not recorded verbatim: %q", got)
	}
}

// TestRecorderFailureDoesNotPanicOrBlock exercises the recorder against a
// session directory that gets its write permission pulled out from under it
// mid-session, simulating a disk/permission failure. Every call into the
// recorder must still return promptly and never propagate an error that a
// caller could mistake for a proxying failure.
func TestRecorderFailureDoesNotPanicOrBlock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix permission semantics assumed")
	}
	if os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}

	r := newTestRecorder(t)
	if err := os.Chmod(r.Dir(), 0o500); err != nil { // read+execute only, no write
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(r.Dir(), 0o700) })

	ex := r.Begin("POST", "/v1/messages", http.Header{"X-Api-Key": {"sk-ant-should-not-matter"}}, []byte(`{"a":1}`))
	rw := ex.ResponseWriter(200, http.Header{"Content-Type": {"text/event-stream"}})
	writeAndClose(t, rw, "data: {}\n\n")

	if err := r.Close(); err != nil {
		t.Fatalf("Close should not surface disk errors to the caller: %v", err)
	}
	// No panic, no hang (t would time out), no error returned. That's the
	// contract: recorder failures are fail-open (internal-docs/05 §5).
}

func TestSetAgentVersion(t *testing.T) {
	r := newTestRecorder(t)
	r.SetAgentVersion("claude-code/1.2.3")
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(r.Dir(), "meta.json"))
	if err != nil {
		t.Fatalf("reading meta.json: %v", err)
	}
	if !strings.Contains(string(data), "claude-code/1.2.3") {
		t.Errorf("agent version not present in meta.json: %s", data)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	r := newTestRecorder(t)
	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestExchangesCount pins the accessor that lets a caller tell an empty
// session from a real one. Both leave a directory, a meta.json and a zero
// exit status, so without this the difference is invisible until someone
// goes looking for a recording that was never captured.
func TestExchangesCount(t *testing.T) {
	r := newTestRecorder(t)

	if got := r.Exchanges(); got != 0 {
		t.Errorf("a fresh recorder reports %d exchanges, want 0", got)
	}
	for i := 1; i <= 3; i++ {
		r.Begin("POST", "/v1/messages", nil, []byte(`{"model":"m"}`))
		if got := r.Exchanges(); got != i {
			t.Errorf("after %d Begin calls, Exchanges() = %d", i, got)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The count has to survive Close: the caller checks it from the same
	// defer that closed the recorder.
	if got := r.Exchanges(); got != 3 {
		t.Errorf("after Close, Exchanges() = %d, want 3", got)
	}
}
