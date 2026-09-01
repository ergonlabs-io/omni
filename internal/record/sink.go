package record

import (
	"bytes"
	"encoding/json"
	"os"
	"sync"
)

// usageTotals holds the token-usage fields we care about, parsed
// best-effort out of a response body. cache_read_input_tokens is the field
// internal-docs/05-constraints.md §1 calls "our canary that omni is not
// silently costing the user money."
type usageTotals struct {
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
}

// maxNonStreamJSONBytes bounds how much of a non-streaming response body the
// sink will buffer in memory to parse for usage. This only affects the
// in-memory usage-parsing best-effort attempt; the full response is always
// written to disk regardless of size.
const maxNonStreamJSONBytes = 8 << 20 // 8MB

// asyncSink is an io.WriteCloser that records response bytes to disk on a
// background goroutine, so a slow or failing disk never adds latency to (or
// aborts) the proxied response copy in front of it.
//
// Write never blocks meaningfully and never returns an error: bytes are
// handed to a buffered channel and, if the background goroutine is falling
// behind, dropped (with a single rate-limited warning) rather than applying
// backpressure to the caller. This is the concrete shape of "record in
// parallel with passthrough, never in front of it" (internal-docs/05 §2) and
// of the fail-open failure policy for record-only mode (internal-docs/05 §5).
type asyncSink struct {
	ch   chan []byte
	done chan struct{}

	warnOnce sync.Once
	exchange int
}

// newAsyncSink starts the background writer. f may be nil (e.g. the file
// could not be created) — bytes are then discarded on disk but usage parsing
// still runs, so the cache-hit canary keeps working even under a broken
// filesystem. sse selects line-buffered SSE parsing vs whole-body JSON
// parsing for the usage extraction. onDone is called exactly once, from the
// background goroutine, with the parsed usage totals once all bytes have
// been processed (i.e. after Close).
func newAsyncSink(f *os.File, sse bool, onDone func(usageTotals), exchange int) *asyncSink {
	s := &asyncSink{
		ch:       make(chan []byte, 64),
		done:     make(chan struct{}),
		exchange: exchange,
	}
	go s.run(f, sse, onDone, exchange)
	return s
}

func (s *asyncSink) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	cp := append([]byte(nil), p...)
	select {
	case s.ch <- cp:
	default:
		s.warnOnce.Do(func() {
			warnf("record: exchange %03d: recorder falling behind, dropping response bytes for this exchange", s.exchange)
		})
	}
	return len(p), nil
}

// Close signals no more writes are coming and blocks until the background
// goroutine has drained everything already queued and closed the file.
func (s *asyncSink) Close() error {
	close(s.ch)
	<-s.done
	return nil
}

func (s *asyncSink) run(f *os.File, sse bool, onDone func(usageTotals), exchange int) {
	defer close(s.done)

	var totals usageTotals
	var lineBuf []byte // partial SSE line, carried across Write calls
	var jsonBuf []byte // whole-body accumulator for the non-SSE case
	jsonOverflow := false

	writeErrored := false
	for b := range s.ch {
		if f != nil && !writeErrored {
			if _, err := f.Write(b); err != nil {
				warnf("record: exchange %03d: response write failed, dropping remaining bytes for this exchange: %v", exchange, err)
				writeErrored = true
			}
		}

		if sse {
			lineBuf = append(lineBuf, b...)
			for {
				i := bytes.IndexByte(lineBuf, '\n')
				if i < 0 {
					break
				}
				line := lineBuf[:i]
				lineBuf = lineBuf[i+1:]
				processSSELine(line, &totals)
			}
		} else if !jsonOverflow {
			if len(jsonBuf)+len(b) > maxNonStreamJSONBytes {
				jsonOverflow = true
				jsonBuf = nil
			} else {
				jsonBuf = append(jsonBuf, b...)
			}
		}
	}

	if sse {
		processSSELine(lineBuf, &totals) // trailing partial line, best-effort
	} else if !jsonOverflow && len(jsonBuf) > 0 {
		applyUsageFromJSON(jsonBuf, &totals)
	}

	if f != nil {
		if err := f.Close(); err != nil {
			warnf("record: exchange %03d: failed closing response file: %v", exchange, err)
		}
	}

	if onDone != nil {
		onDone(totals)
	}
}

// processSSELine handles one line (without its trailing newline) of an SSE
// stream, updating totals if the line is a "data:" line carrying a usage
// object. Anything else (event:, id:, comments, blank lines, non-JSON
// payloads) is silently ignored — this is explicitly best-effort per
// internal-docs' "if the body doesn't parse, skip it, don't fail."
func processSSELine(line []byte, totals *usageTotals) {
	line = bytes.TrimSpace(line)
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "[DONE]" {
		return
	}
	applyUsageFromJSON(data, totals)
}

// applyUsageFromJSON best-effort parses raw as one JSON object and folds any
// usage information found into totals. Anthropic's wire format puts a usage
// object at the top level (non-streaming responses, and SSE message_delta
// events) or nested under "message" (SSE message_start events); we check
// both without needing to know which shape we are looking at. Malformed or
// unrecognized JSON is ignored, never an error.
func applyUsageFromJSON(raw []byte, totals *usageTotals) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return
	}
	if u, ok := obj["usage"]; ok {
		mergeUsage(u, totals)
	}
	if m, ok := obj["message"]; ok {
		var mm map[string]json.RawMessage
		if json.Unmarshal(m, &mm) == nil {
			if u, ok := mm["usage"]; ok {
				mergeUsage(u, totals)
			}
		}
	}
}

// mergeUsage folds one usage object into totals by taking the max seen per
// field. A single exchange's SSE stream can report usage more than once
// (message_start, then message_delta with a growing output_tokens); the max
// per field converges on the final, most-complete count without needing to
// know Anthropic's exact event semantics.
func mergeUsage(raw json.RawMessage, totals *usageTotals) {
	var u struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	}
	if json.Unmarshal(raw, &u) != nil {
		return
	}
	if u.InputTokens > totals.InputTokens {
		totals.InputTokens = u.InputTokens
	}
	if u.OutputTokens > totals.OutputTokens {
		totals.OutputTokens = u.OutputTokens
	}
	if u.CacheCreationInputTokens > totals.CacheCreationInputTokens {
		totals.CacheCreationInputTokens = u.CacheCreationInputTokens
	}
	if u.CacheReadInputTokens > totals.CacheReadInputTokens {
		totals.CacheReadInputTokens = u.CacheReadInputTokens
	}
}
