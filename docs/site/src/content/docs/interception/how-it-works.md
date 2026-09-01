---
title: How interception works
summary: The two data paths — the PTY and the proxy — how traffic is steered
  into omni without touching your system, and what the proxy is allowed to do
  to it.
last_updated: 2026-08-31
related:
  - ../interception/model-routing/
  - ../interception/all-traffic/
  - ../sessions/recorded-sessions/
---

# How interception works

`omni claude` runs two things at once: a proxy on loopback, and the agent as
a child process in a pseudo-terminal, with one environment variable pointing
its API calls at that proxy.

```
your terminal ──PTY──▶ claude ──HTTP──▶ omni ──HTTPS──▶ api.anthropic.com
                          │                 │
                          └─ byte passthrough└─ recorded, then forwarded
```

Those are the two data paths, and they never meet. The terminal path is a
pure byte pipe — it carries ANSI escape sequences from a redrawing TUI, not
structured data, and omni never interprets a byte of it. The network path is
structured, and is the only place omni does anything.

## Steering traffic in

Agents read a base-URL environment variable. omni sets it, for the child
process only:

```sh
ANTHROPIC_BASE_URL=http://127.0.0.1:54312
```

The consequences are worth being explicit about:

- **Nothing is installed.** No certificate authority, no system trust store
  change, no `/etc/hosts` entry, no root.
- **Nothing outside the child is affected.** The variable exists in one
  process tree and disappears when the agent exits.
- **The agent speaks plaintext HTTP to loopback**, and omni speaks TLS to the
  real upstream. TLS is terminated at your machine's own loopback interface,
  by a process you started.
- **omni sees only what goes to that base URL.** Telemetry, update checks,
  and any other endpoint the agent talks to are not intercepted. Seeing those
  too requires [all-traffic mode](../all-traffic/).

This is called Tier 1, and it is what runs by default.

## The terminal path

The agent runs in a real PTY, so it behaves as a TUI: full-screen redraw,
raw-mode keys, `SIGWINCH` on resize, correct exit codes. omni puts your
terminal in raw mode, forwards window-size changes, forwards signals, and
copies bytes in both directions.

Two consequences follow:

- **`omni claude` is indistinguishable from `claude`.** If it is not, that is
  a bug.
- **omni writes nothing to stdout.** Every byte on stdout came from the
  child. omni's own diagnostics go to stderr, and only with `-v`.

When stdin is not a terminal — piped input, CI — omni still allocates a PTY
(the agent needs one to behave at all) but skips raw mode and resize
tracking. The session degrades to non-resizable rather than failing.

## The network path

The proxy is an HTTP server bound to `127.0.0.1` on an ephemeral port,
started before the agent and shut down after it.

**Loopback only, always.** The proxy carries live API credentials in flight.
A configured `proxy.listen` that does not resolve to loopback is refused at
load — not warned about, refused. There is no flag to override it.

**Byte-identical passthrough.** What upstream sends is what the agent
receives. Bodies are never rewritten unless a rule you configured says so,
and never buffered in whole: responses stream through frame by frame, with
each SSE event flushed as it arrives. An agent's streaming UI must look the
same through omni as without it, which means no accumulate-then-forward
anywhere on the response path.

**Timeouts sized for real work.** There is a 30-second header-read timeout
to guard against slow-header attacks, and deliberately no request or
response timeout at all: a long generation or a long tool loop can
legitimately hold a connection open for minutes, and a proxy that kills it is
worse than no proxy.

**Hop-by-hop headers are stripped**, as any correct HTTP intermediary must;
everything else is forwarded untouched.

## The middleware chain

Each request passes through a chain of stages on the way out, and back
through them in reverse on the way in:

```
record ──▶ route ──▶ adapt ──▶ send
```

`record` is outermost on purpose: it is the first to see a request and the
last to see a response, so what it captures is what actually crossed the
wire, not an intermediate form.

Stages come in two tiers. The **raw tier** works on bytes and headers and
never decodes a body — recording, metering, logging. The **decoded tier**
parses the request into a model of the provider's API, which is what routing
and adaptation need.

Today only the raw tier exists, with one stage wired into it: the recorder.

:::note[Phase 0]
`route` and `adapt` are designed and have their seam in the code, but are not
implemented. `--mode route` currently records exactly as `record` does. See
[Model routing](../model-routing/).
:::

## When something fails

The policy differs by what omni is doing, because the cost of being wrong
differs:

- **Recording only — fail open.** A recorder that cannot write, a full disk, a
  slow filesystem: none of these may interrupt your session. Recording runs in
  parallel with the exchange, never in front of it, and a struggling recorder
  drops frames from the capture rather than applying backpressure to the
  response.
- **Actively modifying traffic — fail closed.** If omni is rewriting requests
  and cannot do so correctly, forwarding an unmodified request would silently
  ignore what you configured. It stops instead.

Upstream errors are forwarded verbatim, status and body — a 429 from the
provider must reach the agent's retry logic as a 429. Errors omni itself
originates carry a header that distinguishes them:

```
HTTP/1.1 502 Bad Gateway
X-Omni-Error: upstream-unreachable
```

so an agent, a script, or you can tell "the API failed" from "the proxy
failed" without guessing from the status code.
