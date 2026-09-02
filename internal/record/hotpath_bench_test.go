package record

import (
	"fmt"
	"net/http"
	"testing"
)

// The two calls benchmarked here are the only recorder work that happens
// *synchronously, on the critical path* of a proxied exchange:
//
//	Begin              runs before the request is forwarded upstream
//	Exchange.Response  runs before the client sees the response header
//
// Everything after that — the response body tee — is handed to a background
// goroutine (see asyncSink). These two are what recording actually costs an
// agent turn.

func benchRec(b *testing.B) *Recorder {
	b.Helper()
	r, err := New(b.TempDir(), "claude", "bench")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { r.Close() })
	return r
}

// BenchmarkBegin measures the request-side cost: a JSON header file plus the
// whole request body, both written synchronously before the request is
// forwarded.
func BenchmarkBegin(b *testing.B) {
	h := http.Header{
		"Content-Type":  []string{"application/json"},
		"Authorization": []string{"Bearer sk-ant-xxxxxxxxxxxxxxxxxxxx"},
		"User-Agent":    []string{"claude-cli/1.0"},
	}
	for _, kb := range []int{4, 64, 512} {
		body := make([]byte, kb*1024)
		b.Run(fmt.Sprintf("body=%dKB", kb), func(b *testing.B) {
			r := benchRec(b)
			b.ResetTimer()
			for range b.N {
				r.Begin("POST", "/v1/messages", h, body)
			}
		})
	}
}

// BenchmarkExchangeResponseWriter measures the response-side cost: a JSON
// header file and the response file creation, both of which happen before
// the client's WriteHeader — so this lands directly in time-to-first-byte.
func BenchmarkExchangeResponseWriter(b *testing.B) {
	h := http.Header{"Content-Type": []string{"text/event-stream"}}
	r := benchRec(b)
	// One Begin, outside the timer: this benchmark is about the response
	// side alone. Re-opening the same exchange's files each iteration is
	// fine — the cost being measured is the create, not the content.
	ex := r.Begin("POST", "/v1/messages", nil, nil)
	b.ResetTimer()
	for range b.N {
		w := ex.ResponseWriter(200, h)
		w.Close()
	}
}

// BenchmarkBeginEmpty isolates Begin's fixed cost — two file creates and a
// JSON marshal — from the cost of writing the body bytes.
func BenchmarkBeginEmpty(b *testing.B) {
	r := benchRec(b)
	b.ResetTimer()
	for range b.N {
		r.Begin("POST", "/v1/messages", nil, nil)
	}
}

// BenchmarkAsyncSinkWrite measures the per-frame cost of the response tee —
// what each SSE token costs while streaming. This one is off the critical
// path in the sense that it never blocks on disk, but it does run inline in
// the proxy's copy loop, so it must stay cheap.
func BenchmarkAsyncSinkWrite(b *testing.B) {
	frame := []byte("event: content_block_delta\ndata: {\"delta\":{\"text\":\"hello\"}}\n\n")
	r := benchRec(b)
	ex := r.Begin("POST", "/v1/messages", nil, nil)
	w := ex.ResponseWriter(200, http.Header{"Content-Type": []string{"text/event-stream"}})
	defer w.Close()
	b.ResetTimer()
	for range b.N {
		w.Write(frame)
	}
}
