package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ergonlabs-io/omni/internal/record"
)

// startProxy builds and starts a Server against upstream, returning it along
// with a teardown func. Fails the test on any setup error.
func startProxy(t *testing.T, upstream *httptest.Server, rec *record.Recorder) *Server {
	t.Helper()
	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parsing upstream URL: %v", err)
	}
	s, err := New(Config{Upstream: u, Recorder: rec})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.Shutdown(ctx)
	})
	return s
}

func TestNonLoopbackBindRejected(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)

	cases := []string{"0.0.0.0:0", "8.8.8.8:0", ":0", "192.168.1.5:9000"}
	for _, addr := range cases {
		t.Run(addr, func(t *testing.T) {
			_, err := New(Config{Upstream: u, ListenAddr: addr})
			if err == nil {
				t.Fatalf("New with ListenAddr %q: expected an error rejecting non-loopback bind, got nil", addr)
			}
		})
	}
}

func TestLoopbackBindAccepted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream.Close()

	s := startProxy(t, upstream, nil)
	if s.Addr() == "" {
		t.Fatalf("Addr() empty after Start")
	}
	if !strings.HasPrefix(s.Addr(), "127.0.0.1:") {
		t.Errorf("Addr() = %q, want 127.0.0.1:PORT", s.Addr())
	}
	if !strings.HasPrefix(s.URL(), "http://127.0.0.1:") {
		t.Errorf("URL() = %q, want http://127.0.0.1:PORT", s.URL())
	}
}

func TestFailOpenOnUpstream500(t *testing.T) {
	body := `{"type":"error","error":{"type":"internal_server_error","message":"boom"}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Marker", "yes")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, body)
	}))
	defer upstream.Close()

	s := startProxy(t, upstream, nil)

	resp, err := http.Get(s.URL() + "/v1/messages")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if resp.Header.Get("X-Omni-Error") != "" {
		t.Errorf("X-Omni-Error set on a genuine upstream error; it must only mark omni-originated errors")
	}
	if resp.Header.Get("X-Upstream-Marker") != "yes" {
		t.Errorf("upstream header not forwarded on error response")
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(got) != body {
		t.Errorf("body mismatch:\n got: %s\nwant: %s", got, body)
	}
}

func TestOmniOriginatedErrorCarriesHeader(t *testing.T) {
	// Point the proxy at an upstream URL nothing is listening on, so the
	// round trip itself fails (as opposed to upstream returning a normal
	// error status).
	u, _ := url.Parse("http://127.0.0.1:1") // port 1: nothing listens here
	s, err := New(Config{Upstream: u})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.Shutdown(ctx)
	}()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(s.URL() + "/v1/messages")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get(OmniErrorHeader) == "" {
		t.Errorf("expected %s header on an omni-originated failure (upstream unreachable), got none; status=%d", OmniErrorHeader, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}

func TestByteIdenticalPassthroughNonStreaming(t *testing.T) {
	reqBody := []byte(`{"model":"claude-opus-4-6","max_tokens":100,"messages":[{"role":"user","content":"hi there"}]}`)
	respBody := []byte(`{"id":"msg_01abc","type":"message","content":[{"type":"text","text":"hello!"}],"usage":{"input_tokens":10,"output_tokens":5}}`)

	var gotReqBody []byte
	var gotReqHeader http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReqBody, _ = io.ReadAll(r.Body)
		gotReqHeader = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respBody)
	}))
	defer upstream.Close()

	s := startProxy(t, upstream, nil)

	req, err := http.NewRequest(http.MethodPost, s.URL()+"/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", "sk-ant-test-passthrough-should-forward-verbatim")
	req.Header.Set("Anthropic-Version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	gotRespBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}

	if !bytes.Equal(gotReqBody, reqBody) {
		t.Errorf("request body mutated:\n got: %s\nwant: %s", gotReqBody, reqBody)
	}
	if !bytes.Equal(gotRespBody, respBody) {
		t.Errorf("response body mutated:\n got: %s\nwant: %s", gotRespBody, respBody)
	}
	if got := gotReqHeader.Get("X-Api-Key"); got != "sk-ant-test-passthrough-should-forward-verbatim" {
		t.Errorf("credential header not forwarded verbatim to upstream: got %q", got)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	wantCL := fmt.Sprintf("%d", len(respBody))
	if cl := resp.Header.Get("Content-Length"); cl != "" && cl != wantCL {
		t.Errorf("Content-Length = %q, want %q", cl, wantCL)
	}
}

func TestByteIdenticalPassthroughStreaming(t *testing.T) {
	frames := []string{
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"hel\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"lo\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, f := range frames {
			io.WriteString(w, f)
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	s := startProxy(t, upstream, nil)

	resp, err := http.Get(s.URL() + "/v1/messages")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	want := strings.Join(frames, "")
	if string(got) != want {
		t.Errorf("streamed body not byte-identical:\n got: %q\nwant: %q", got, want)
	}
}

// TestSSEFramesFlushedIndividually is the timing-sensitive test doc
// 07-testing.md calls out explicitly: a content-only comparison passes even
// on a fully buffered proxy. This test asserts that frames arrive at the
// client spaced out over time, matching the pace the upstream server wrote
// (and flushed) them at, rather than all at once at the end.
func TestSSEFramesFlushedIndividually(t *testing.T) {
	const (
		frameCount = 4
		frameDelay = 150 * time.Millisecond
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for i := 0; i < frameCount; i++ {
			fmt.Fprintf(w, "data: {\"i\":%d}\n\n", i)
			flusher.Flush()
			if i < frameCount-1 {
				time.Sleep(frameDelay)
			}
		}
	}))
	defer upstream.Close()

	s := startProxy(t, upstream, nil)

	resp, err := http.Get(s.URL() + "/v1/messages")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	var arrival []time.Time
	start := time.Now()
	for i := 0; i < frameCount; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading frame %d: %v (line so far: %q)", i, err, line)
		}
		if !strings.HasPrefix(line, "data:") {
			// second line of the frame ("data: ..."), consume the blank
			// separator line too before recording arrival.
			i--
			continue
		}
		arrival = append(arrival, time.Now())
		reader.ReadString('\n') // consume the blank line terminating the SSE event
	}

	if len(arrival) != frameCount {
		t.Fatalf("got %d frame arrivals, want %d", len(arrival), frameCount)
	}

	// If the proxy buffered the whole response, every frame would arrive
	// within a few milliseconds of `start` (right after upstream finished
	// writing, ~450ms in). Instead we expect roughly one frame every
	// frameDelay, so total elapsed time should be close to
	// (frameCount-1)*frameDelay, and — the real test — no two consecutive
	// frames should arrive back-to-back for the whole stream.
	total := arrival[len(arrival)-1].Sub(start)
	minExpected := time.Duration(frameCount-1) * frameDelay * 60 / 100 // generous slack
	if total < minExpected {
		t.Errorf("all frames arrived too close together (total spread %v, want at least %v) — looks like the proxy buffered the stream instead of flushing per frame", total, minExpected)
	}

	for i := 1; i < len(arrival); i++ {
		gap := arrival[i].Sub(arrival[i-1])
		if gap < frameDelay/3 {
			t.Errorf("frame %d arrived only %v after frame %d (expected ~%v) — looks buffered", i, gap, i-1, frameDelay)
		}
	}
}

func TestHopByHopHeadersStripped(t *testing.T) {
	var gotConnectionHeader, gotCustomHopHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotConnectionHeader = r.Header.Get("Connection")
		gotCustomHopHeader = r.Header.Get("X-Should-Be-Hopped")

		w.Header().Set("Connection", "close")
		w.Header().Set("X-Regular-Header", "keep-me")
		body := []byte("plain body")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer upstream.Close()

	s := startProxy(t, upstream, nil)

	req, _ := http.NewRequest(http.MethodGet, s.URL()+"/v1/models", nil)
	// Declare X-Should-Be-Hopped as a hop-by-hop header via Connection, per
	// RFC 7230 §6.1 — httputil.ReverseProxy strips headers named in
	// Connection in addition to the standard hop-by-hop set.
	req.Header.Set("Connection", "X-Should-Be-Hopped")
	req.Header.Set("X-Should-Be-Hopped", "should-not-arrive")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if gotConnectionHeader != "" {
		t.Errorf("Connection header forwarded to upstream: %q", gotConnectionHeader)
	}
	if gotCustomHopHeader != "" {
		t.Errorf("header named in Connection was forwarded to upstream: %q", gotCustomHopHeader)
	}
	if resp.Header.Get("Connection") != "" {
		t.Errorf("Connection header present on client response, should have been stripped")
	}
	if resp.Header.Get("X-Regular-Header") != "keep-me" {
		t.Errorf("non-hop-by-hop response header was stripped")
	}
	if got := resp.ContentLength; got != int64(len("plain body")) {
		t.Errorf("Content-Length = %d, want %d", got, len("plain body"))
	}
	if string(body) != "plain body" {
		t.Errorf("body = %q, want %q", body, "plain body")
	}
}

func TestSlowUpstreamDoesNotTripTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow-upstream test in -short mode")
	}
	const delay = 3 * time.Second
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay) // simulate a long "thinking" delay before first byte
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	s := startProxy(t, upstream, nil)

	client := &http.Client{Timeout: delay + 10*time.Second}
	resp, err := client.Get(s.URL() + "/v1/messages")
	if err != nil {
		t.Fatalf("GET failed — proxy appears to have imposed a timeout shorter than a plausible tool-loop delay: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestRecorderRedactionThroughProxy exercises redaction end-to-end through
// the full Server (RecordingMiddleware wired to a real Recorder), not just
// the record package in isolation. No credential-shaped string may appear
// anywhere under the session directory afterward.
func TestRecorderRedactionThroughProxy(t *testing.T) {
	const (
		apiKey = "sk-ant-api03-PROXY_INTEGRATION_FAKE_KEY_000000"
		oauth  = "sk-ant-oat01-PROXY_INTEGRATION_FAKE_OAUTH_00000"
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		io.WriteString(w, `data: {"type":"message_start","message":{"usage":{"cache_read_input_tokens":10}}}`+"\n\n")
		flusher.Flush()
		io.WriteString(w, "event: message_stop\ndata: {}\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	tmp := t.TempDir()
	rec, err := record.New(filepath.Join(tmp, "sessions"), "claude", "omni-test/0.0.0")
	if err != nil {
		t.Fatalf("record.New: %v", err)
	}

	s := startProxy(t, upstream, rec)

	req, _ := http.NewRequest(http.MethodPost, s.URL()+"/v1/messages", bytes.NewReader([]byte(`{"model":"claude-opus-4-6"}`)))
	req.Header.Set("Authorization", "Bearer "+oauth)
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Anthropic-Beta", "oauth-2025-04-20")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if err := rec.Close(); err != nil {
		t.Fatalf("rec.Close: %v", err)
	}

	entries, err := os.ReadDir(rec.Dir())
	if err != nil {
		t.Fatalf("reading session dir: %v", err)
	}
	secrets := []string{apiKey, oauth, "Bearer " + oauth}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(rec.Dir(), e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		for _, secret := range secrets {
			if strings.Contains(string(data), secret) {
				t.Errorf("file %s leaked credential material via the live proxy path: %q", e.Name(), secret)
			}
		}
	}
}

// TestRecorderFailureDoesNotBreakProxying points the proxy at a Recorder
// whose session directory has been made unwritable mid-session (simulating
// disk/permission failure), then asserts the client still receives a
// complete, correct response — the whole point of doc 05 §5's fail-open
// policy for record-only mode.
func TestRecorderFailureDoesNotBreakProxying(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix permission semantics assumed")
	}
	if os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}

	respBody := []byte(`{"id":"msg_ok","type":"message","content":[{"type":"text","text":"still works"}]}`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(respBody)
	}))
	defer upstream.Close()

	tmp := t.TempDir()
	rec, err := record.New(filepath.Join(tmp, "sessions"), "claude", "omni-test/0.0.0")
	if err != nil {
		t.Fatalf("record.New: %v", err)
	}
	if err := os.Chmod(rec.Dir(), 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(rec.Dir(), 0o700) })

	s := startProxy(t, upstream, rec)

	resp, err := http.Post(s.URL()+"/v1/messages", "application/json", bytes.NewReader([]byte(`{"a":1}`)))
	if err != nil {
		t.Fatalf("POST failed even though only the recorder is broken: %v", err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if !bytes.Equal(got, respBody) {
		t.Errorf("body mismatch despite recorder failure:\n got: %s\nwant: %s", got, respBody)
	}

	if err := rec.Close(); err != nil {
		t.Fatalf("rec.Close should not surface disk errors: %v", err)
	}
}

// TestLargeStreamingBodyPassesThroughIntact is a light sanity check that
// nothing in the recording tee path (which copies chunks) drops or
// reorders bytes for a larger multi-chunk stream.
func TestLargeStreamingBodyPassesThroughIntact(t *testing.T) {
	payload := make([]byte, 512*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunk := 4096
		for i := 0; i < len(payload); i += chunk {
			end := min(i+chunk, len(payload))
			w.Write(payload[i:end])
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer upstream.Close()

	tmp := t.TempDir()
	rec, err := record.New(filepath.Join(tmp, "sessions"), "claude", "omni-test/0.0.0")
	if err != nil {
		t.Fatalf("record.New: %v", err)
	}
	s := startProxy(t, upstream, rec)

	resp, err := http.Get(s.URL() + "/v1/messages")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("large streamed body corrupted through the recording tee (len got=%d want=%d)", len(got), len(payload))
	}
}
