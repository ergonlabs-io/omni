// Package proxy implements omni's interception proxy: an HTTP server bound
// to loopback that stands between a coding agent and its real upstream API.
//
// Tier 1 only: no TLS MITM and no CONNECT tunneling — those are later
// phases. A faithful, byte-identical, non-buffering passthrough is the
// baseline; see internal-docs/05-constraints.md §§1-3 for why that is harder
// than it sounds.
//
// On top of that baseline, [RoutingMiddleware] implements model rewriting
// and per-model backend selection (mode = "route"). Routing is opt-in, and
// a request no route claims is forwarded exactly as byte-identically as it
// would be with routing switched off. Capability adaptation is still a
// later phase.
package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/ergonlabs-io/omni/internal/record"
)

// OmniErrorHeader is set on any response omni itself originates, as opposed
// to one forwarded verbatim from upstream. Doc 05 §5: "Whatever we return
// should be distinguishable from an upstream error." A caller (or the
// agent's own retry logic) can key off this header rather than guessing from
// status code alone.
const OmniErrorHeader = "X-Omni-Error"

// defaultListenAddr is used when Config.ListenAddr is empty: loopback,
// ephemeral port.
const defaultListenAddr = "127.0.0.1:0"

// Config configures a Server.
type Config struct {
	// Upstream is the real API endpoint every request is forwarded to, e.g.
	// https://api.anthropic.com. Required.
	Upstream *url.URL

	// Recorder, if non-nil, receives a tee of every request and response.
	// Nil disables recording entirely (and, as a bonus, avoids ever
	// buffering the request body in memory — see [RecordingMiddleware]).
	Recorder *record.Recorder

	// ListenAddr overrides the default "127.0.0.1:0" (loopback, ephemeral
	// port). Any address whose host is not a loopback address is rejected by
	// New — internal-docs/05 §7: "No listening on non-loopback interfaces,
	// ever." A proxy holding live Anthropic credentials must not be
	// reachable off-host.
	ListenAddr string

	// ExtraMiddleware is applied inside the recorder, ahead of the terminal
	// reverse-proxy handler. This is the seam routing hooks into (see
	// [RoutingMiddleware]) and where adaptation will land.
	ExtraMiddleware []RawMiddleware
}

// Server is omni's loopback interception proxy.
type Server struct {
	upstream   *url.URL
	listenAddr string

	httpServer *http.Server

	mu   sync.Mutex
	ln   net.Listener
	addr string
}

// New validates cfg and builds a Server. It does not bind a listener or
// start serving — call [Server.Start] for that, once you're ready to learn
// the resolved address.
func New(cfg Config) (*Server, error) {
	if cfg.Upstream == nil {
		return nil, errors.New("proxy: Config.Upstream is required")
	}

	listenAddr := cfg.ListenAddr
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	if err := checkLoopback(listenAddr); err != nil {
		return nil, err
	}

	s := &Server{
		upstream:   cfg.Upstream,
		listenAddr: listenAddr,
	}

	rp := s.newReverseProxy(cfg.Upstream)

	terminal := RawHandler(rp.ServeHTTP)
	mw := append([]RawMiddleware{RecordingMiddleware(cfg.Recorder)}, cfg.ExtraMiddleware...)
	handler := Chain(terminal, mw...)

	s.httpServer = &http.Server{
		Handler: http.HandlerFunc(handler),
		// ReadHeaderTimeout guards against a slow-headers attack; it does not
		// affect body streaming (request or response), which can legitimately
		// run for minutes during a long tool loop — internal-docs/05 §2/§5.
		ReadHeaderTimeout: 30 * time.Second,
		// No ReadTimeout/WriteTimeout: both would cap the *entire*
		// request/response lifetime in net/http, which would silently kill a
		// long-running SSE generation or tool loop. Deliberately left at the
		// zero value (no limit) rather than inherited without thought.
		IdleTimeout: 120 * time.Second,
	}

	return s, nil
}

// newReverseProxy builds the terminal handler: an httputil.ReverseProxy
// configured for byte-identical, immediately-flushed passthrough to
// upstream.
func (s *Server) newReverseProxy(upstream *url.URL) *httputil.ReverseProxy {
	transport := &http.Transport{
		// The Anthropic API is served over h2 (internal-docs/05 §3).
		// net/http's Transport bundles its own HTTP/2 support (the same code
		// as golang.org/x/net/http2, vendored in) and negotiates it
		// automatically over TLS via ALPN — but only when it's confident
		// nothing about the Transport's config was customized to avoid it.
		// Since we do set a custom TLSClientConfig below, ForceAttemptHTTP2
		// is required to keep h2 on rather than silently falling back to h1
		// (see the ForceAttemptHTTP2 doc comment in net/http).
		ForceAttemptHTTP2:     true,
		Proxy:                 nil, // never honor HTTP_PROXY/HTTPS_PROXY env for upstream traffic
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		// No ResponseHeaderTimeout: a "thinking" model can legitimately take
		// a long time to produce its first byte. Capping this would turn a
		// slow-but-healthy generation into a spurious 502 mid tool-loop.
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// SetURL alone (no SetXForwarded) rewrites scheme/host/path to
			// upstream and fixes the outbound Host header, without adding or
			// altering any header the client didn't send. That is the whole
			// byte-identical-passthrough contract for headers — see doc 05
			// §1: any injected field can bust a cache_control breakpoint
			// downstream of it, and more generally an unrouted request must
			// be mutated in no way at all.
			target := upstream
			if b := backendFor(pr.In); b != nil {
				// A route claimed this request. The routing middleware has
				// already swapped the credential; all that is left is to
				// point it at the backend's host.
				target = b.URL
			}
			pr.SetURL(target)
		},
		Transport: transport,
		// Flush after every write, immediately. This is the single most
		// important line in this file: without it, a buffered io.Copy makes
		// the agent's TUI look frozen for the whole generation (doc 05 §2).
		FlushInterval: -1,
		ErrorHandler:  s.errorHandler,
	}
	return rp
}

// errorHandler runs when the round trip to upstream itself fails (DNS,
// connection refused, TLS failure, context canceled, etc — not an upstream
// HTTP error status, which passes through untouched as a normal response).
// Per doc 05 §5, this is an error omni originates, so it is marked with
// OmniErrorHeader and returns 502 rather than anything upstream might have
// returned for the same request.
func (s *Server) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	warnf("proxy: round trip to upstream failed: %v", err)
	w.Header().Set(OmniErrorHeader, "upstream-unreachable")
	w.WriteHeader(http.StatusBadGateway)
}

// checkLoopback rejects any listen address that is not explicitly loopback.
// No DNS resolution is performed (deliberately: resolution can be
// non-deterministic and we would rather reject an ambiguous host than risk
// binding somewhere reachable off-host).
func checkLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("proxy: invalid listen address %q: %w", addr, err)
	}
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return nil
	case "":
		// e.g. ":8080" — binds all interfaces. Explicitly rejected.
		return fmt.Errorf("proxy: refusing to bind %q — omni holds live credentials and must stay on loopback only", addr)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("proxy: refusing to bind non-loopback address %q — omni holds live credentials and must stay on loopback only", addr)
}

// Start binds the listener and begins serving in the background. It returns
// once the listener is bound, so [Server.Addr] is valid immediately after
// Start returns without error.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("proxy: listen on %q: %w", s.listenAddr, err)
	}

	s.mu.Lock()
	s.ln = ln
	s.addr = ln.Addr().String()
	s.mu.Unlock()

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			warnf("proxy: serve error: %v", err)
		}
	}()
	return nil
}

// Addr returns the resolved "host:port" the server is listening on. Only
// meaningful after a successful [Server.Start].
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// URL returns the base URL an agent should be pointed at, e.g.
// "http://127.0.0.1:54321". This is plaintext HTTP by design — Tier 1
// interception (internal-docs/02-architecture.md) has the agent speak
// plaintext to omni, while omni speaks TLS to the real upstream. No CA, no
// trust-store surgery.
func (s *Server) URL() string {
	return "http://" + s.Addr()
}

// Shutdown gracefully stops the server, waiting for in-flight requests (up
// to ctx's deadline) to complete.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
