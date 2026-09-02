package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ergonlabs-io/omni/internal/glob"
	"github.com/ergonlabs-io/omni/internal/record"
)

// Backend is a resolved destination the router can send a request to: a
// parsed base URL, a credential already read from the environment, and any
// extra headers the endpoint wants.
//
// This is a plain value, not a config type. internal/proxy deliberately
// knows nothing about TOML, layering, or provenance — cmd/omni resolves
// configuration into these and hands them over.
type Backend struct {
	// Name is used only in diagnostics.
	Name string
	// URL is the destination base, e.g. https://openrouter.ai/api. The
	// client's request path is appended to it.
	URL *url.URL
	// APIKey is the credential for this backend, already read from its
	// configured environment variable. Sent as "Authorization: Bearer".
	// Empty means send no credential at all — a local inference server.
	APIKey string
	// PreserveAuth forwards the request's own credentials and provider
	// headers untouched. Set only when this backend resolves to the same
	// host the agent would have reached anyway, which is what makes
	// stripping meaningless. Never set for a third party.
	PreserveAuth bool
	// Headers are extra headers set on every request to this backend.
	Headers map[string]string
}

// Rule is one routing rule: when the agent asks for a model matching Match,
// send Model instead (or the same model, if Model is empty) to Backend (or
// to the default upstream, when Backend is nil).
type Rule struct {
	// Match is a glob against the model the agent asked for.
	Match string
	// Model replaces the requested model. Empty leaves it unchanged, which
	// is what a pure change-of-destination rule wants.
	Model string
	// Backend is the destination, or nil for the agent's default upstream.
	Backend *Backend
}

// Router holds the ordered rule list. First match wins, in list order —
// glob patterns overlap, so order is part of the meaning, not an
// implementation detail.
type Router struct {
	rules []Rule
	// OnRoute, if set, is called once per rewritten request. cmd/omni uses
	// it for --verbose diagnostics; it must not write to stdout.
	OnRoute func(from, to, backend string)
	// OnRouteFailure, if set, is called when a request this router claimed
	// comes back with an error status. Like OnRoute it is a --verbose
	// diagnostic channel and must not write to stdout.
	//
	// This exists because omni is the only component that can connect the
	// two halves of the story: the agent knows it asked for one model and
	// got a 401, and the backend knows it rejected a credential, but only
	// omni knows a rule sent the one to the other. Without it a dead key in
	// a routed backend surfaces as nothing but the agent's own retry loop.
	OnRouteFailure func(RouteFailure)
}

// RouteFailure describes a request that a routing rule claimed and that the
// destination answered with an error status.
type RouteFailure struct {
	// From is the model the agent asked for; To is the model actually sent
	// (the same string when the rule only changed destination).
	From, To string
	// Backend names the destination, or is empty when the rule kept the
	// agent's default upstream.
	Backend string
	// Status is the HTTP status the destination returned.
	Status int
	// Body is a short, sanitized excerpt of the error body: whitespace
	// collapsed, non-printables dropped, the backend's own credential
	// redacted if it was echoed back. Empty when no excerpt could safely be
	// taken (a streaming or content-encoded response).
	Body string
}

// NewRouter builds a Router from an ordered rule list. It returns nil when
// rules is empty, which callers can pass straight to RoutingMiddleware — a
// nil Router is a no-op, so "routing is off" needs no special case.
func NewRouter(rules []Rule) *Router {
	if len(rules) == 0 {
		return nil
	}
	return &Router{rules: rules}
}

// lookup returns the first rule whose pattern matches model.
func (rt *Router) lookup(model string) (Rule, bool) {
	if rt == nil {
		return Rule{}, false
	}
	for _, r := range rt.rules {
		if glob.Match(r.Match, model) {
			return r, true
		}
	}
	return Rule{}, false
}

// routeKey is the context key under which a claimed request's routing
// decision travels from the routing middleware to the reverse proxy — to
// Rewrite, which needs the backend, and to ModifyResponse, which needs the
// rest to describe a failure.
//
// The failure callback rides along in the context rather than being read off
// the Router because the two ends are not connected: ModifyResponse hangs off
// the Server, which has no idea a Router exists. The request is the only
// thing both of them hold.
type routeKeyType struct{}

var routeKey routeKeyType

// routeInfo is what a matched rule records about its decision.
type routeInfo struct {
	from    string
	to      string
	backend *Backend
	onFail  func(RouteFailure)
}

// routeFor returns the routing decision made for r, or nil when no rule
// claimed it.
func routeFor(r *http.Request) *routeInfo {
	if r == nil {
		return nil
	}
	ri, _ := r.Context().Value(routeKey).(*routeInfo)
	return ri
}

// backendFor returns the backend selected for r, or nil for the default
// upstream.
func backendFor(r *http.Request) *Backend {
	if ri := routeFor(r); ri != nil {
		return ri.backend
	}
	return nil
}

// routableRequest reports whether this request carries an Anthropic
// Messages body worth inspecting. Everything else — GETs, /v1/models, the
// OAuth endpoints, anything omni does not model — passes through
// untouched. Rewriting is opt-in per path, never best-effort.
func routableRequest(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	p := strings.TrimSuffix(r.URL.Path, "/")
	return strings.HasSuffix(p, "/v1/messages") ||
		strings.HasSuffix(p, "/v1/messages/count_tokens")
}

// RoutingMiddleware rewrites the model name on Anthropic Messages requests
// and, when the matched route names a backend, retargets the request to it.
//
// A nil Router is a no-op passthrough, so record-only sessions pay nothing.
//
// Ordering matters: this sits *inside* RecordingMiddleware, so the session
// on disk holds the request as the agent wrote it, not as omni rewrote it.
// That is the ordering internal-docs/02-architecture.md specifies ("record
// first out, last in") and it is the more useful of the two — the recording
// answers "what did the agent ask for", and the route diagnostic answers
// "what did omni do about it".
func RoutingMiddleware(rt *Router) RawMiddleware {
	if rt == nil {
		return func(next RawHandler) RawHandler { return next }
	}
	return func(next RawHandler) RawHandler {
		return func(w http.ResponseWriter, r *http.Request) {
			if !routableRequest(r) {
				next(w, r)
				return
			}

			body, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err != nil {
				// Fail open, exactly as the recorder does: forward what we
				// have rather than turning a read hiccup into an outage.
				warnf("routing: reading request body: %v — forwarding unrouted", err)
				r.Body = io.NopCloser(bytes.NewReader(body))
				r.ContentLength = int64(len(body))
				next(w, r)
				return
			}

			_, _, model, err := modelField(body)
			if err != nil {
				if err != errNoModel {
					warnf("routing: cannot read model from request: %v — forwarding unrouted", err)
				}
				restoreBody(r, body)
				next(w, r)
				return
			}

			rule, ok := rt.lookup(model)
			if !ok {
				restoreBody(r, body)
				next(w, r)
				return
			}

			target := model
			if rule.Model != "" && rule.Model != model {
				rewritten, err := replaceModel(body, rule.Model)
				if err != nil {
					// The model was readable a moment ago, so this is a bug,
					// not bad input. Forward the original: a request that
					// reaches the wrong model is worse than one that reaches
					// the right one at the wrong price.
					warnf("routing: rewriting model %q -> %q failed: %v — forwarding unrouted", model, rule.Model, err)
					restoreBody(r, body)
					next(w, r)
					return
				}
				body = rewritten
				target = rule.Model
			}
			restoreBody(r, body)

			if rule.Backend != nil {
				applyBackendAuth(r, rule.Backend)
			}
			// Attached for every matched rule, not only the ones naming a
			// backend: a pure model rename is still a decision omni made on
			// the agent's behalf, so a failure that follows it is still worth
			// attributing. Rewrite reads only the backend field, which stays
			// nil in that case and leaves the default upstream in place.
			r = r.WithContext(context.WithValue(r.Context(), routeKey, &routeInfo{
				from:    model,
				to:      target,
				backend: rule.Backend,
				onFail:  rt.OnRouteFailure,
			}))

			if rt.OnRoute != nil {
				name := ""
				if rule.Backend != nil {
					name = rule.Backend.Name
				}
				rt.OnRoute(model, target, name)
			}

			next(w, r)
		}
	}
}

// restoreBody puts b back on r as a re-readable body with a correct length.
// Content-Length must be reset explicitly: a rewritten model name changes
// the body size, and a stale header would truncate the request or hang the
// upstream waiting for bytes that never come.
func restoreBody(r *http.Request, b []byte) {
	r.Body = io.NopCloser(bytes.NewReader(b))
	r.ContentLength = int64(len(b))
	if r.Header.Get("Content-Length") != "" {
		r.Header.Set("Content-Length", strconv.Itoa(len(b)))
	}
}

// stripCredentials removes every header carrying the agent's own credential,
// using the same predicate the recorder redacts by
// ([record.IsCredentialHeader]) rather than a second hand-maintained list.
//
// Claude Code sends `Authorization` today — 328 recorded requests, no other
// credential header among them — but it sends `x-api-key` instead when
// authenticated with ANTHROPIC_API_KEY rather than OAuth, and the predicate
// covers provider variants ending in -api-key without anyone having to
// enumerate them.
func stripCredentials(h http.Header) {
	var drop []string
	for k := range h {
		if record.IsCredentialHeader(k) {
			drop = append(drop, k)
		}
	}
	for _, k := range drop {
		h.Del(k)
	}
}

// applyBackendAuth swaps the agent's credential for the backend's own.
//
// Credential headers are the *only* thing removed, and `Authorization` has to
// be rewritten regardless, since that is where the backend's key goes.
// Everything else the agent sent — anthropic-version, anthropic-beta and its
// feature gates, the x-stainless-* client telemetry, user-agent — is
// forwarded verbatim, on the same principle as the rest of the proxy: omni
// does not decide on the agent's behalf which headers a backend wants. A
// backend that rejects one says so, and a rejection you can see beats a
// header omni silently ate.
//
// PreserveAuth short-circuits even the credential swap: the destination is
// the same host the agent would have reached anyway, so no boundary is being
// crossed. cmd/omni is the only thing that sets it, and only after comparing
// hosts — see config.Backend.Policy.
func applyBackendAuth(r *http.Request, b *Backend) {
	if b.PreserveAuth {
		for k, v := range b.Headers {
			r.Header.Set(k, v)
		}
		return
	}
	stripCredentials(r.Header)
	if b.APIKey != "" {
		r.Header.Set("Authorization", "Bearer "+b.APIKey)
	}
	for k, v := range b.Headers {
		r.Header.Set(k, v)
	}
}
