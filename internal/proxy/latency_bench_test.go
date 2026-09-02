package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ergonlabs-io/omni/internal/record"
)

// sseUpstream is a stand-in for the Messages API: it replies with an SSE
// stream of n frames, flushing each one, the way a real streaming completion
// arrives. gap simulates inter-token time so time-to-first-byte is
// measurable separately from total stream duration.
func sseUpstream(frames int, gap time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f, _ := w.(http.Flusher)
		for i := range frames {
			fmt.Fprintf(w, "event: content_block_delta\ndata: {\"delta\":{\"text\":\"tok%d\"}}\n\n", i)
			if f != nil {
				f.Flush()
			}
			if gap > 0 {
				time.Sleep(gap)
			}
		}
		fmt.Fprint(w, "event: message_stop\ndata: {\"usage\":{\"output_tokens\":10}}\n\n")
		if f != nil {
			f.Flush()
		}
	}))
}

// benchBody is a stand-in for an agent's request body: the whole
// conversation, which grows through a session. Claude Code routinely sends
// hundreds of KB here.
func benchBody(n int) []byte {
	b := bytes.Repeat([]byte(`{"role":"user","content":"xxxxxxxxxxxxxxxx"},`), n/44+1)
	return append([]byte(`{"messages":[`), append(b, ']', '}')...)
}

// timeToFirstByte issues one request through the proxy and reports how long
// until the first response byte lands, and how long until the stream ends.
// For an interactive agent, TTFB is what a person actually perceives.
func timeToFirstByte(b *testing.B, client *http.Client, target string, body []byte) (ttfb, total time.Duration) {
	b.Helper()
	start := time.Now()
	req, err := http.NewRequest("POST", target+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		b.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		b.Fatal(err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 1)
	if _, err := resp.Body.Read(buf); err != nil && err != io.EOF {
		b.Fatal(err)
	}
	ttfb = time.Since(start)
	io.Copy(io.Discard, resp.Body)
	return ttfb, time.Since(start)
}

// benchRecorder builds a Recorder writing into b.TempDir(), or nil for the
// recording-disabled arm.
func benchRecorder(b *testing.B, on bool) *record.Recorder {
	b.Helper()
	if !on {
		return nil
	}
	rec, err := record.New(b.TempDir(), "claude", "bench")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { rec.Close() })
	return rec
}

// BenchmarkRecordingLatency measures what turning recording on costs an
// agent session, across request-body sizes. The interesting number is the
// delta between the record=off and record=on arms at the same body size.
func BenchmarkRecordingLatency(b *testing.B) {
	for _, bodyKB := range []int{4, 64, 512} {
		body := benchBody(bodyKB * 1024)
		for _, on := range []bool{false, true} {
			name := fmt.Sprintf("body=%dKB/record=%v", bodyKB, on)
			b.Run(name, func(b *testing.B) {
				up := sseUpstream(20, 0)
				defer up.Close()
				u, _ := url.Parse(up.URL)
				s, err := New(Config{Upstream: u, Recorder: benchRecorder(b, on)})
				if err != nil {
					b.Fatal(err)
				}
				if err := s.Start(); err != nil {
					b.Fatal(err)
				}
				defer s.Shutdown(b.Context())

				client := &http.Client{}
				target := s.URL()

				// Warm the connection so we measure steady state, not dial.
				timeToFirstByte(b, client, target, body)

				var ttfbSum, totalSum time.Duration
				b.ResetTimer()
				for range b.N {
					ttfb, total := timeToFirstByte(b, client, target, body)
					ttfbSum += ttfb
					totalSum += total
				}
				b.StopTimer()
				if b.N > 0 {
					b.ReportMetric(float64(ttfbSum.Microseconds())/float64(b.N), "us/ttfb")
					b.ReportMetric(float64(totalSum.Microseconds())/float64(b.N), "us/total")
				}
			})
		}
	}
}
