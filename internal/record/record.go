// Package record captures the request/response traffic omni's proxy forwards
// between a coding agent and its upstream API, writing it to disk as a
// session corpus for later analysis. See internal-docs/07-testing.md — this
// corpus is what every later phase (adapter golden files, cache-regression
// checks) is built against.
//
// Two invariants shape the whole package:
//
//   - Credential redaction is ON by default and is a positive opt-out only
//     (internal-docs/05-constraints.md §6). The corpus gets committed to
//     testdata/; a leaked key in git history is unrecoverable.
//   - Recording never breaks proxying. Every write path here is fail-open:
//     errors are logged to stderr and swallowed, never returned to a caller
//     in a way that could abort an in-flight response (internal-docs/05
//     §5). Package record never writes to stdout (internal-docs/09 §3).
package record

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Recorder captures one agent session's traffic to a directory on disk. It is
// safe for concurrent use: multiple exchanges (HTTP requests) may be in
// flight at once, as they are for a real agent doing parallel tool calls.
type Recorder struct {
	dir         string
	agentName   string
	omniVersion string
	now         func() time.Time

	redact bool

	mu           sync.Mutex
	seq          int
	agentVersion string
	usage        usageTotals
	closed       bool

	startTime time.Time

	wg sync.WaitGroup // outstanding async response sinks, waited on by Close
}

// Option configures a Recorder at construction time.
type Option func(*Recorder)

// WithRedaction controls credential redaction of recorded headers.
// Redaction is ON by default; this exists to let a caller turn it OFF
// explicitly. There is deliberately no way to disable redaction other than
// this explicit call — see internal-docs/05-constraints.md §6: "Un-redaction
// is an explicit opt-in flag, never the default."
func WithRedaction(enabled bool) Option {
	return func(r *Recorder) { r.redact = enabled }
}

// WithAgentVersion records the agent's version string in the session's
// meta.json, when known at construction time. It can also be set later via
// [Recorder.SetAgentVersion].
func WithAgentVersion(v string) Option {
	return func(r *Recorder) { r.agentVersion = v }
}

// withClock overrides the time source, for tests.
func withClock(now func() time.Time) Option {
	return func(r *Recorder) { r.now = now }
}

// New creates a new session recording directory under baseDir and returns a
// Recorder writing into it.
//
// baseDir is taken as a parameter and used as-is: New does not resolve
// $OMNI_HOME or ~/.omni — that is internal/config's job. Callers pass
// "<omniHome>/sessions" (or an equivalent). baseDir is created if it does not
// exist.
//
// The session directory is named "<timestamp>-<agentName>", e.g.
// "2026-08-30T08-17-02-claude" (colons are filesystem-hostile, hence the
// hyphenated time format). omniVersion is recorded in meta.json so a captured
// corpus can be traced back to the omni build that produced it.
func New(baseDir, agentName, omniVersion string, opts ...Option) (*Recorder, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("record: baseDir must not be empty")
	}
	if agentName == "" {
		return nil, fmt.Errorf("record: agentName must not be empty")
	}

	r := &Recorder{
		agentName:   agentName,
		omniVersion: omniVersion,
		redact:      true, // on by default; see WithRedaction doc.
		now:         time.Now,
	}
	for _, opt := range opts {
		opt(r)
	}

	r.startTime = r.now()

	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, fmt.Errorf("record: creating session base directory: %w", err)
	}

	dir, err := makeSessionDir(baseDir, sessionDirName(r.startTime, agentName))
	if err != nil {
		return nil, fmt.Errorf("record: creating session directory: %w", err)
	}
	r.dir = dir

	// Write an initial meta.json immediately, so a session that crashes or is
	// killed before Close still leaves a trace of when it started and with
	// what agent. Close overwrites it with the final summary.
	if err := writeJSONFile(filepath.Join(r.dir, "meta.json"), r.meta(nil)); err != nil {
		warnf("record: failed writing initial meta.json: %v", err)
	}

	return r, nil
}

// sessionDirName formats the "<timestamp>-<agent>" directory name.
func sessionDirName(t time.Time, agent string) string {
	return t.Format("2006-01-02T15-04-05") + "-" + agent
}

// makeSessionDir creates baseDir/name, retrying with a numeric suffix on
// collision (e.g. two sessions for the same agent starting within the same
// second) rather than silently reusing an existing directory.
func makeSessionDir(baseDir, name string) (string, error) {
	path := filepath.Join(baseDir, name)
	if err := os.Mkdir(path, 0o700); err == nil {
		return path, nil
	} else if !os.IsExist(err) {
		return "", err
	}
	for i := 2; i < 1000; i++ {
		path = filepath.Join(baseDir, fmt.Sprintf("%s-%d", name, i))
		err := os.Mkdir(path, 0o700)
		if err == nil {
			return path, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("could not allocate a unique session directory under %q", baseDir)
}

// Dir returns the session's recording directory.
func (r *Recorder) Dir() string { return r.dir }

// SetAgentVersion records the agent's version string, once known, for the
// final meta.json written by Close. Safe to call at any point before Close.
func (r *Recorder) SetAgentVersion(v string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentVersion = v
}

// Exchange represents one in-flight request/response pair being recorded.
// Obtained from [Recorder.Begin].
type Exchange struct {
	r *Recorder
	n int
}

// Begin starts recording one request/response exchange: it writes the
// redacted request headers and the verbatim request body to disk, and
// returns an Exchange used to record the eventual response.
//
// Begin is fail-open (internal-docs/05 §5): if writing fails, it logs to
// stderr and returns an Exchange that safely no-ops for the rest of its
// lifetime. Callers never need to special-case recorder errors — recording
// failures must never affect the proxied request/response.
//
// body is the exact bytes of the request body; header is the request's
// headers before any redaction (Begin redacts its own copy before writing).
func (r *Recorder) Begin(method, url string, header http.Header, body []byte) *Exchange {
	r.mu.Lock()
	r.seq++
	n := r.seq
	r.mu.Unlock()

	ex := &Exchange{r: r, n: n}

	rec := requestRecord{
		Method: method,
		URL:    url,
		Header: r.redactHeader(header),
	}
	if err := writeJSONFile(r.exchangePath(n, "request.headers.json"), rec); err != nil {
		warnf("record: exchange %03d: failed writing request headers: %v", n, err)
	}
	if err := os.WriteFile(r.exchangePath(n, "request.json"), body, 0o600); err != nil {
		warnf("record: exchange %03d: failed writing request body: %v", n, err)
	}

	return ex
}

// ResponseWriter returns an io.WriteCloser that records the response body as
// it streams through the proxy. status and header are the response's status
// line and headers as they are about to be sent to the client (header is
// used, among other things, to detect text/event-stream and choose the
// ".sse" vs ".json" file extension per internal-docs/07's corpus layout).
//
// Writes to the returned writer never block the caller for long (they hand
// bytes to a background goroutine) and never return an error — a struggling
// or failed recorder degrades to dropping frames for this exchange, not to
// interrupting the proxied response. This is what "record in parallel with
// passthrough, never in front of it" (internal-docs/05 §2) means in practice.
//
// The caller must Close the returned writer exactly once, after the response
// body is fully copied (or the copy aborts), to flush and release resources.
// Close does not itself return interesting file errors — those are logged as
// they happen.
func (e *Exchange) ResponseWriter(status int, header http.Header) io.WriteCloser {
	r := e.r
	sse := isEventStream(header.Get("Content-Type"))
	ext := "json"
	if sse {
		ext = "sse"
	}

	rec := responseRecord{
		Status: status,
		Header: r.redactHeader(header),
	}
	if err := writeJSONFile(r.exchangePath(e.n, "response.headers.json"), rec); err != nil {
		warnf("record: exchange %03d: failed writing response headers: %v", e.n, err)
	}

	f, err := os.OpenFile(r.exchangePath(e.n, "response."+ext), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		warnf("record: exchange %03d: failed creating response file: %v", e.n, err)
		f = nil // fall through: still track usage even if we can't persist bytes.
	}

	r.wg.Add(1)
	n := e.n
	return newAsyncSink(f, sse, func(u usageTotals) {
		r.addUsage(u)
		r.wg.Done()
	}, n)
}

func (r *Recorder) addUsage(u usageTotals) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usage.InputTokens += u.InputTokens
	r.usage.OutputTokens += u.OutputTokens
	r.usage.CacheCreationInputTokens += u.CacheCreationInputTokens
	r.usage.CacheReadInputTokens += u.CacheReadInputTokens
}

func (r *Recorder) exchangePath(n int, suffix string) string {
	return filepath.Join(r.dir, fmt.Sprintf("%03d.%s", n, suffix))
}

// Close finalizes the session: waits for any in-flight response recording to
// flush, then writes the final meta.json (start/end time, exchange count, and
// the usage summary — including the cache_read_input_tokens totals that are
// our canary for prompt-cache health per internal-docs/05 §1). Safe to call
// once; a second call is a no-op.
//
// Close should be called from the same defer that shuts down the proxy
// (internal-docs/02's lifecycle), so a session's corpus is guaranteed
// complete on process exit.
func (r *Recorder) Close() error {
	r.wg.Wait()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true

	end := r.now()
	return writeJSONFile(filepath.Join(r.dir, "meta.json"), r.meta(&end))
}

// meta must be called with r.mu held, except from New (before any other
// goroutine can see r).
func (r *Recorder) meta(end *time.Time) sessionMeta {
	return sessionMeta{
		Agent:        r.agentName,
		AgentVersion: r.agentVersion,
		OmniVersion:  r.omniVersion,
		Redacted:     r.redact,
		StartTime:    r.startTime,
		EndTime:      end,
		Exchanges:    r.seq,
		Summary: summary{
			InputTokensTotal:              r.usage.InputTokens,
			OutputTokensTotal:             r.usage.OutputTokens,
			CacheCreationInputTokensTotal: r.usage.CacheCreationInputTokens,
			CacheReadInputTokensTotal:     r.usage.CacheReadInputTokens,
		},
	}
}

type sessionMeta struct {
	Agent        string     `json:"agent"`
	AgentVersion string     `json:"agent_version,omitempty"`
	OmniVersion  string     `json:"omni_version,omitempty"`
	Redacted     bool       `json:"redacted"`
	StartTime    time.Time  `json:"start_time"`
	EndTime      *time.Time `json:"end_time,omitempty"`
	Exchanges    int        `json:"exchanges"`
	Summary      summary    `json:"summary"`
}

// summary is the per-session usage rollup. cache_read_input_tokens is called
// out in internal-docs/05-constraints.md §1 as "our canary that omni is not
// silently costing the user money": if it stays at zero across a multi-turn
// session, the prompt cache prefix is being invalidated somewhere.
type summary struct {
	InputTokensTotal              int64 `json:"input_tokens_total"`
	OutputTokensTotal             int64 `json:"output_tokens_total"`
	CacheCreationInputTokensTotal int64 `json:"cache_creation_input_tokens_total"`
	CacheReadInputTokensTotal     int64 `json:"cache_read_input_tokens_total"`
}

type requestRecord struct {
	Method string              `json:"method"`
	URL    string              `json:"url"`
	Header map[string][]string `json:"header"`
}

type responseRecord struct {
	Status int                 `json:"status"`
	Header map[string][]string `json:"header"`
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}

func warnf(format string, args ...any) {
	// stderr only. Package record must never write to stdout — see
	// internal-docs/09-cli-design.md §3: any stdout byte from omni corrupts
	// the child TUI's display.
	fmt.Fprintf(os.Stderr, "omni: "+format+"\n", args...)
}
