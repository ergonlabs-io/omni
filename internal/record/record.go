// Package record captures the request/response traffic omni's proxy forwards
// between a coding agent and its upstream API, writing it to disk as a
// session corpus for later analysis. See internal-docs/07-testing.md — this
// corpus is what every later phase (adapter golden files, cache-regression
// checks) is built against.
//
// Three invariants shape the whole package:
//
//   - Credential redaction is ON by default and is a positive opt-out only
//     (internal-docs/05-constraints.md §6). The corpus gets committed to
//     testdata/; a leaked key in git history is unrecoverable.
//   - Recording never breaks proxying. Every write path here is fail-open:
//     errors are logged to stderr and swallowed, never returned to a caller
//     in a way that could abort an in-flight response (internal-docs/05
//     §5). Package record never writes to stdout (internal-docs/09 §3).
//   - Bodies stay verbatim, in their own files. The round-trip gate in
//     internal-docs/07 §Tier 1 requires the exact bytes off the wire, so a
//     body is never re-encoded, and never escaped into a JSON string where
//     jq, git diff, and a human reading testdata/ could no longer see it.
//     Everything *about* an exchange — headers, status, timing — goes to the
//     session's exchanges.jsonl index instead.
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

// exchangeLogName is the session's append-only index: one JSON object per
// line, two lines per exchange (a "request" event and a "response" event,
// each written as soon as its headers are known so a killed session still
// leaves an accurate partial index). It replaces the per-exchange
// NNN.request.headers.json / NNN.response.headers.json pair, which doubled
// the file count without ever being read on its own.
const exchangeLogName = "exchanges.jsonl"

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

	// logMu guards log and is deliberately separate from mu: appending an
	// index line must not contend with the sink goroutines calling addUsage,
	// and holding one lock while taking the other is what would make that a
	// deadlock rather than just contention.
	logMu sync.Mutex
	log   *os.File

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

	// The index is opened once and appended to for the life of the session.
	// Failing to open it is fail-open like every other write path here: the
	// bodies still land on disk, and the session loses its index rather than
	// its traffic.
	if f, ferr := os.OpenFile(filepath.Join(r.dir, exchangeLogName),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); ferr != nil {
		warnf("record: failed creating %s, exchange metadata will not be indexed: %v", exchangeLogName, ferr)
	} else {
		r.log = f
	}

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
	r     *Recorder
	n     int
	start time.Time
}

// Begin starts recording one request/response exchange: it writes the
// verbatim request body to its own file and appends a "request" line to the
// session's exchanges.jsonl carrying the redacted headers. It returns an
// Exchange used to record the eventual response.
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

	start := r.now()
	ex := &Exchange{r: r, n: n, start: start}

	bodyFile := exchangeFile(n, "request.json")
	if err := os.WriteFile(filepath.Join(r.dir, bodyFile), body, 0o600); err != nil {
		warnf("record: exchange %03d: failed writing request body: %v", n, err)
	}

	r.appendEvent(exchangeEvent{
		Seq:      n,
		Type:     "request",
		Time:     start,
		Method:   method,
		URL:      url,
		Header:   r.redactHeader(header),
		BodyFile: bodyFile,
	})

	return ex
}

// ResponseWriter returns an io.WriteCloser that records the response body as
// it streams through the proxy. status and header are the response's status
// line and headers as they are about to be sent to the client (header is
// used, among other things, to detect text/event-stream and choose the
// ".sse" vs ".json" file extension per internal-docs/07's corpus layout).
//
// The matching "response" line is appended to exchanges.jsonl here, when the
// headers are known rather than when the body finishes: that keeps the index
// accurate for a session killed mid-stream, and it makes the recorded
// ttfb_ms an actual time-to-first-byte for a streaming response.
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

	bodyFile := exchangeFile(e.n, "response."+ext)
	at := r.now()
	r.appendEvent(exchangeEvent{
		Seq:      e.n,
		Type:     "response",
		Time:     at,
		Status:   status,
		TTFBMs:   at.Sub(e.start).Milliseconds(),
		Header:   r.redactHeader(header),
		BodyFile: bodyFile,
	})

	f, err := os.OpenFile(filepath.Join(r.dir, bodyFile), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
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

// appendEvent writes one line to exchanges.jsonl. Like every other write
// here it is fail-open: an encoding or write failure costs this session one
// index line, never the proxied exchange.
func (r *Recorder) appendEvent(ev exchangeEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		warnf("record: exchange %03d: failed encoding %s event: %v", ev.Seq, ev.Type, err)
		return
	}
	b = append(b, '\n')

	r.logMu.Lock()
	defer r.logMu.Unlock()
	if r.log == nil {
		return // could not be opened, or the session is already closed.
	}
	if _, err := r.log.Write(b); err != nil {
		warnf("record: exchange %03d: failed appending %s event: %v", ev.Seq, ev.Type, err)
	}
}

// exchangeFile names an exchange's body file. It is a bare filename rather
// than a path because it is also what the index's body_file field carries,
// and that field must stay meaningful after the session directory is moved
// into testdata/.
func exchangeFile(n int, suffix string) string {
	return fmt.Sprintf("%03d.%s", n, suffix)
}

// Close finalizes the session: waits for any in-flight response recording to
// flush, closes the exchange index, then writes the final meta.json
// (start/end time, exchange count, and the usage summary — including the
// cache_read_input_tokens totals that are our canary for prompt-cache health
// per internal-docs/05 §1). Safe to call once; a second call is a no-op.
//
// Close should be called from the same defer that shuts down the proxy
// (internal-docs/02's lifecycle), so a session's corpus is guaranteed
// complete on process exit.
func (r *Recorder) Close() error {
	r.wg.Wait()

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	end := r.now()
	m := r.meta(&end)
	r.mu.Unlock()

	r.logMu.Lock()
	if r.log != nil {
		if cerr := r.log.Close(); cerr != nil {
			warnf("record: closing %s: %v", exchangeLogName, cerr)
		}
		r.log = nil // later appendEvent calls become no-ops rather than panics.
	}
	r.logMu.Unlock()

	return writeJSONFile(filepath.Join(r.dir, "meta.json"), m)
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

// exchangeEvent is one line of exchanges.jsonl. Request-only and
// response-only fields share the struct and are omitempty, so a line stays
// readable and a consumer can filter on .type without a schema:
//
//	jq 'select(.type == "response" and .status != 200)' exchanges.jsonl
//
// Body bytes are deliberately absent — BodyFile names the sibling file
// holding them verbatim. See the package doc's third invariant.
type exchangeEvent struct {
	Seq  int       `json:"seq"`
	Type string    `json:"type"` // "request" or "response"
	Time time.Time `json:"time"`

	// Request-only.
	Method string `json:"method,omitempty"`
	URL    string `json:"url,omitempty"`

	// Response-only. TTFBMs is measured from the request event, so for a
	// streamed response it is time-to-first-byte, not total duration.
	Status int   `json:"status,omitempty"`
	TTFBMs int64 `json:"ttfb_ms,omitempty"`

	Header   map[string][]string `json:"header"`
	BodyFile string              `json:"body_file"`
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
