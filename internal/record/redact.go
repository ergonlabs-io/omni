package record

import (
	"net/http"
	"strings"
)

// redactedValue replaces a credential header's value in recorded output. The
// header key is preserved so a captured session still shows which auth shape
// was used (x-api-key vs Authorization: Bearer) without leaking the secret.
const redactedValue = "[REDACTED]"

// IsCredentialHeader reports whether name carries a credential, per
// internal-docs/05-constraints.md §6: "Redact Authorization, x-api-key, and
// *-api-key in the recorder by default." This also catches provider-specific
// variants such as "anthropic-api-key".
//
// Exported because the recorder is not the only thing that needs the
// definition: the routing middleware must strip exactly these before
// forwarding to another backend. One predicate, two call sites — if the set
// omni redacts on disk ever drifted from the set it refuses to forward, one
// of the two would be wrong, and the forwarding side is the one that leaks.
func IsCredentialHeader(name string) bool {
	n := strings.ToLower(name)
	switch n {
	case "authorization", "x-api-key":
		return true
	}
	return strings.HasSuffix(n, "-api-key")
}

// redactHeader returns a copy of h suitable for writing to disk: credential
// headers have their values replaced, unless the Recorder was explicitly
// constructed with WithRedaction(false). Every other header is copied
// verbatim.
func (r *Recorder) redactHeader(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		if r.redact && IsCredentialHeader(k) {
			out[k] = []string{redactedValue}
			continue
		}
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

// isEventStream reports whether a Content-Type value denotes an SSE stream.
func isEventStream(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}
