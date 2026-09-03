package config

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/ergonlabs-io/omni/internal/suggest"
)

// backendNameRe constrains backend names to a boring, shell-safe charset.
// The name appears in config keys, in rules, and in error messages; keeping
// it narrow means it never needs quoting or escaping.
var backendNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Backend is one declared destination a routing rule can target: a base
// URL, optionally the environment variable holding its credential, the wire
// format it speaks, and the model to ask it for.
//
// Backends are global (~/.omni/omni.conf); rules are per-agent. An endpoint
// is an endpoint, but "claude-opus-5" is meaningless to Codex — see
// internal-docs/08-configuration.md §Routing and backends.
//
// A project-local .omni.conf can never declare a backend. Declaring one is
// declaring somewhere your prompts may be sent, which is not a decision a
// repository you cloned gets to make for you.
type Backend struct {
	// Name is the key under [backends.<name>].
	Name string
	// BaseURL is the scheme://host[/path] requests are forwarded to. The
	// agent's request path is appended, so an Anthropic-native backend
	// serving /v1/messages is declared as the part before /v1.
	BaseURL string
	// APIKeyEnv names the environment variable holding this backend's
	// credential. At launch omni looks it up in its own environment and,
	// failing that, in ~/.omni/credentials — never in config, which is what
	// makes this a variable *name* rather than a key. Empty is legal only
	// for a loopback endpoint (a local model server wanting no auth) or for
	// a backend that is the agent's own upstream — see AuthPolicy.
	APIKeyEnv string
	// APIStyle is the wire format this backend speaks. It must match the
	// agent's own style: omni routes, it does not translate (see
	// internal-docs/04-interception.md §Cross-provider).
	APIStyle string
	// Model is what to actually ask this backend for, when a rule targeting
	// it does not name a model of its own.
	Model string
	// Headers are extra headers set on every request to this backend, for
	// endpoints that want attribution (OpenRouter's HTTP-Referer, X-Title).
	Headers map[string]string
	// Source is the file:line that declared this backend.
	Source string
}

// rawBackend mirrors a [backends.<name>] table.
type rawBackend struct {
	BaseURL   *string           `toml:"base_url"`
	APIKeyEnv *string           `toml:"api_key_env"`
	APIStyle  *string           `toml:"api_style"`
	Model     *string           `toml:"model"`
	Headers   map[string]string `toml:"headers"`
}

// knownBackendPaths are the recognized dotted keys inside a
// [backends.<name>] table. headers is a wildcard: its right-hand sides are
// user-chosen header names, not schema.
var knownBackendPaths = map[string]bool{
	"base_url":    true,
	"api_key_env": true,
	"api_style":   true,
	"model":       true,
}

var knownBackendWildcards = []string{"headers"}

// AuthPolicy says what omni does with the agent's own inbound credential
// when forwarding to a backend.
type AuthPolicy int

const (
	// AuthSubstitute strips the agent's credential and sends the backend's
	// own, from APIKeyEnv. The normal case for a third party.
	AuthSubstitute AuthPolicy = iota
	// AuthPreserve forwards the request's credentials untouched. Only ever
	// chosen when the backend resolves to the same host the agent would
	// have reached anyway, which makes "stripping" meaningless — this is
	// what lets [backends.anthropic] be declared and targeted explicitly.
	AuthPreserve
	// AuthNone strips the agent's credential and sends none at all: a local
	// inference server that wants no authentication.
	AuthNone
)

// Policy decides how to treat the agent's credential when forwarding to b.
// agentUpstream is the agent profile's default upstream URL.
//
// The rule is host-based rather than name-based on purpose. Whether a
// credential may travel is a question about *where the bytes are going*,
// and a config that named a backend "anthropic" and pointed it elsewhere
// must not be able to talk omni into leaking a token there.
func (b Backend) Policy(agentUpstream string) AuthPolicy {
	if b.APIKeyEnv != "" {
		return AuthSubstitute
	}
	if sameHost(b.BaseURL, agentUpstream) {
		return AuthPreserve
	}
	return AuthNone
}

// sameHost reports whether two URLs address the same host and port.
func sameHost(a, b string) bool {
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false
	}
	return ua.Host != "" && ua.Host == ub.Host
}

// applyBackends overlays the [backends] table onto e. Unlike the scalar
// settings this is a whole-table merge by backend name: two layers may each
// declare different backends, but a later layer redeclaring a name replaces
// it entirely rather than merging field by field. A half-overridden backend
// (this layer's base_url, that layer's api_key_env) is a configuration
// nobody can reason about.
func applyBackends(e *Effective, raw map[string]rawBackend, loc locator, issues *[]Issue) {
	if len(raw) == 0 {
		return
	}
	merged := make(map[string]Backend, len(e.Backends.V)+len(raw))
	for k, v := range e.Backends.V {
		merged[k] = v
	}
	for name, rb := range raw {
		path := "backends." + name
		source := loc(path)

		if !backendNameRe.MatchString(name) {
			*issues = append(*issues, Issue{
				Path:    path,
				Message: fmt.Sprintf("invalid backend name %q (want lowercase letters, digits, '-' or '_')", name),
				Source:  source,
				Level:   LevelError,
			})
			continue
		}

		b := Backend{Name: name, Source: source, Headers: rb.Headers}

		if rb.BaseURL == nil || *rb.BaseURL == "" {
			*issues = append(*issues, Issue{
				Path:    path + ".base_url",
				Message: fmt.Sprintf("backend %q has no base_url", name),
				Source:  source,
				Level:   LevelError,
			})
			continue
		}
		b.BaseURL = strings.TrimRight(*rb.BaseURL, "/")
		if iss := validateBaseURL(path+".base_url", b.BaseURL, source); iss != nil {
			*issues = append(*issues, *iss)
			continue
		}
		if iss := checkBaseURLPath(path+".base_url", b.BaseURL, source); iss != nil {
			*issues = append(*issues, *iss)
		}

		if rb.APIKeyEnv != nil {
			b.APIKeyEnv = *rb.APIKeyEnv
		}
		if rb.Model != nil {
			b.Model = *rb.Model
		}

		// Default to the only style omni can currently route natively.
		b.APIStyle = "anthropic"
		if rb.APIStyle != nil {
			b.APIStyle = *rb.APIStyle
		}
		if !validAPIStyle(b.APIStyle) {
			*issues = append(*issues, Issue{
				Path:    path + ".api_style",
				Message: fmt.Sprintf("invalid api_style %q (want \"anthropic\" or \"openai\")", b.APIStyle),
				Source:  source,
				Level:   LevelError,
			})
			continue
		}

		merged[name] = b
	}
	e.Backends = Value[map[string]Backend]{merged, loc("backends")}
}

func validAPIStyle(s string) bool { return s == "anthropic" || s == "openai" }

// validateBaseURL rejects a backend URL omni cannot safely forward to.
// Plaintext http is allowed only for loopback: a backend URL may carry a
// live API key in its Authorization header, and sending that in the clear
// to anywhere but this machine is a credential leak, not a preference.
func validateBaseURL(path, raw, source string) *Issue {
	u, err := url.Parse(raw)
	if err != nil {
		return &Issue{
			Path:    path,
			Message: fmt.Sprintf("invalid base_url %q: %s", raw, err),
			Source:  source,
			Level:   LevelError,
		}
	}
	if u.Host == "" {
		return &Issue{
			Path:    path,
			Message: fmt.Sprintf("base_url %q has no host", raw),
			Source:  source,
			Level:   LevelError,
		}
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackAddr(u.Host) {
			return nil
		}
		return &Issue{
			Path: path,
			Message: fmt.Sprintf(
				"base_url %q is plaintext http to a non-loopback host — omni may send this "+
					"backend's API key on every request and will not transmit it unencrypted",
				raw,
			),
			Source: source,
			Level:  LevelFatal,
		}
	default:
		return &Issue{
			Path:    path,
			Message: fmt.Sprintf("base_url %q must be https (or http on loopback), got scheme %q", raw, u.Scheme),
			Source:  source,
			Level:   LevelError,
		}
	}
}

// agentRequestSuffixes are the path tails a base_url must not already end
// with, longest first so the reported fix strips the whole thing. Anthropic
// agents request /v1/messages, OpenAI-style ones /v1/chat/completions; both
// share the /v1 that makes the bare version worth catching too.
var agentRequestSuffixes = []string{
	"/v1/messages/count_tokens",
	"/v1/chat/completions",
	"/v1/messages",
	"/v1",
}

// checkBaseURLPath catches a base_url that already contains the path the
// agent is going to send.
//
// omni forwards by joining the agent's own request path onto base_url
// (proxy.Server's Rewrite calls httputil.ProxyRequest.SetURL, which joins
// rather than replaces). So the backend written the way every provider
// documents its own base URL — "https://openrouter.ai/api/v1", which is
// therefore what people paste — makes omni request /api/v1/v1/messages.
//
// Backends answer that with a 404, and the 404 is usually an HTML page from
// the provider's *website* rather than a JSON error from its API, so the
// agent renders it as something unrelated to the URL: Claude Code reports it
// as "the selected model may not exist". Nothing in that message points at
// base_url, which makes this expensive to diagnose from the agent's side —
// and it is entirely predictable from the config alone, which is what earns
// it a check. Before this, `config check` called such a config "ok".
//
// It is a Warning, not an Error: omni does not rewrite a URL the user wrote,
// and a backend genuinely serving /v1/v1/messages is absurd rather than
// impossible. Naming the problem and the fix is the whole job; silently
// correcting the URL would be the worse habit.
func checkBaseURLPath(path, raw, source string) *Issue {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil // validateBaseURL already reported this
	}
	trimmed := strings.TrimRight(u.Path, "/")
	if trimmed == "" {
		return nil
	}
	for _, suffix := range agentRequestSuffixes {
		if !strings.HasSuffix(trimmed, suffix) {
			continue
		}
		// Show the URL the agent's own request would actually produce, and
		// the base_url that fixes it. A message that only says "looks wrong"
		// leaves the reader to work out the join rule for themselves.
		broken := fmt.Sprintf("%s://%s%s/v1/messages", u.Scheme, u.Host, trimmed)
		fixed := strings.TrimSuffix(raw, "/")
		fixed = strings.TrimSuffix(fixed, suffix)
		return &Issue{
			Path: path,
			Message: fmt.Sprintf(
				"base_url %q already ends in %s, but omni appends the agent's own "+
					"request path — requests would go to %s, which backends answer "+
					"with 404. Drop it: base_url = %q",
				raw, suffix, broken, fixed,
			),
			Source: source,
			Level:  LevelWarning,
		}
	}
	return nil
}

func didYouMeanBackend(got string, declared map[string]Backend) string {
	near := suggest.Near(got, sortedBackendNames(declared))
	if len(near) == 0 {
		return ""
	}
	return " (did you mean: " + strings.Join(near, ", ") + "?)"
}

func sortedBackendNames(m map[string]Backend) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
