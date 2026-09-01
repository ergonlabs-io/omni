package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/ergonlabs-io/omni/internal/record"
)

// RawHandler serves an HTTP request against the raw byte/header tier: it
// never decodes the Messages API body. It is the extension point later
// phases build on — see internal-docs/02-architecture.md "Middleware chain".
type RawHandler func(w http.ResponseWriter, r *http.Request)

// RawMiddleware wraps a RawHandler with behavior that only needs bytes and
// headers, never a decoded body — logging, metering, recording. Phase 0
// wires exactly one built-in: [RecordingMiddleware]. The decoded tier
// (routing, adaptation) is a later phase; there is no schema package yet to
// build it against, so this package intentionally stops at raw.
type RawMiddleware func(next RawHandler) RawHandler

// Chain composes middleware around a terminal handler. mw[0] is outermost:
// it sees the request first and, on the way back, is the last to see the
// response — exactly the "record must be first out, last in" ordering from
// internal-docs/02-architecture.md.
func Chain(final RawHandler, mw ...RawMiddleware) RawHandler {
	h := final
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// RecordingMiddleware returns a RawMiddleware that tees every request and
// response through rec. Passing a nil rec returns a pure no-op passthrough —
// when recording is disabled, the request body is never even read into
// memory, so it streams straight through to the terminal handler untouched.
//
// Recording failures never affect the proxied exchange: see
// [record.Recorder] for the fail-open contract this relies on.
func RecordingMiddleware(rec *record.Recorder) RawMiddleware {
	if rec == nil {
		return func(next RawHandler) RawHandler { return next }
	}
	return func(next RawHandler) RawHandler {
		return func(w http.ResponseWriter, r *http.Request) {
			var body []byte
			if r.Body != nil && r.Body != http.NoBody {
				b, err := io.ReadAll(r.Body)
				r.Body.Close()
				if err != nil {
					// We couldn't fully read the body to record it. Fail open:
					// forward whatever we did read rather than dropping the
					// request. This does mean this exchange's recorded request
					// (if anything at all downstream still reads r.Body) may be
					// partial; it will not be byte-identical, but the live
					// request/response to the real client is unaffected because
					// we still forward exactly what we read.
					warnf("proxy: failed reading request body for recording: %v", err)
				}
				body = b
				r.Body = io.NopCloser(bytes.NewReader(body))
				r.ContentLength = int64(len(body))
			}

			ex := rec.Begin(r.Method, r.URL.String(), r.Header, body)
			rw := &recordingResponseWriter{ResponseWriter: w, ex: ex}
			defer rw.Close() // must run even if next panics (e.g. client disconnect mid-stream)
			next(rw, r)
		}
	}
}

// recordingResponseWriter wraps a client-facing http.ResponseWriter so every
// byte written to the client is also (asynchronously, non-blockingly) teed
// to the session recorder. It forwards Write/Flush to the real
// ResponseWriter first, then tees — the recorder never sits in front of the
// client on the response path (internal-docs/05 §2).
type recordingResponseWriter struct {
	http.ResponseWriter
	ex *record.Exchange

	wroteHeader bool
	sink        io.WriteCloser // nil until WriteHeader, and if ex is nil
}

func (rw *recordingResponseWriter) WriteHeader(status int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	if rw.ex != nil {
		// rw.Header() already holds the full response header set at this
		// point: httputil.ReverseProxy copies upstream's headers into it
		// before calling WriteHeader.
		rw.sink = rw.ex.ResponseWriter(status, rw.Header().Clone())
	}
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *recordingResponseWriter) Write(p []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(p) // client first
	if rw.sink != nil && n > 0 {
		rw.sink.Write(p[:n]) // then a non-blocking, error-swallowing tee
	}
	return n, err
}

// Flush implements http.Flusher so httputil.ReverseProxy's per-frame
// flushing (FlushInterval: -1) reaches the real connection. Without this,
// wrapping the ResponseWriter would silently defeat SSE streaming.
func (rw *recordingResponseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController see through this wrapper to whatever
// richer interfaces (e.g. http.Pusher) the underlying ResponseWriter
// implements.
func (rw *recordingResponseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

// Close flushes and releases the response-side recorder resources. Safe to
// call even if WriteHeader was never reached (e.g. a panic before any
// response was sent) — Close is then a no-op.
func (rw *recordingResponseWriter) Close() {
	if rw.sink != nil {
		rw.sink.Close()
	}
}

func warnf(format string, args ...any) {
	// stderr only — see internal-docs/09-cli-design.md §3.
	fmt.Fprintf(os.Stderr, "omni: "+format+"\n", args...)
}
