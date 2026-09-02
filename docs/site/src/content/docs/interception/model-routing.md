---
title: Model routing
summary: How [[route]] rules and backends work, why routing must be
  deterministic, and where the credential boundary sits.
last_updated: 2026-08-31
related:
  - ../interception/how-it-works/
  - ../configuration/configuration-file/
  - ../sessions/recorded-sessions/
---

# Model routing

Routing rewrites the model name on the wire, and can send the request to a
different provider entirely: the agent asks for one model, omni forwards a
request for another, somewhere else.

```toml
# ~/.omni/omni.conf
[agents.claude]

[[agents.claude.route]]
match = "claude-opus-*"
model = "claude-sonnet-5"
```

```sh
omni --mode route --model-map 'claude-opus-*=claude-sonnet-5' claude
```

## Rules

Rules are an **ordered list, first match wins**, in the order written.
`match` is a glob against the model the agent asked for, and nothing else.

```toml
[[agents.claude.route]]
match   = "claude-haiku-4-5*"    # the alias and the dated id, in one rule
backend = "openrouter"

[[agents.claude.route]]
match = "claude-opus-*"
model = "claude-sonnet-5"        # same provider, cheaper model
```

A rule sends the request to a `backend`, renames the model in place with
`model`, or both. When a rule names a backend but no model, the backend's own
`model` is used. Anything no rule matches goes to the agent's own upstream,
byte for byte, exactly as if routing were off.

The list is ordered rather than a table because globs overlap and TOML tables
have no defined iteration order — a map of patterns would resolve differently
from run to run. `omni config check` warns about a rule an earlier rule
already covers, since it can never fire.

The glob syntax is the small one: `*` matches any run of characters
(including `/` and `:`), `?` matches one, everything else is literal.

## Backends

A backend is a destination a rule can target. Backends are global — an
endpoint is an endpoint — while rules are per-agent, because `claude-opus-5`
is meaningless to Codex.

```toml
[backends.openrouter]
base_url    = "https://openrouter.ai/api"   # serves /v1/messages natively
api_key_env = "OPENROUTER_API_KEY"          # the name; never the key itself
api_style   = "anthropic"
model       = "minimax/minimax-m3:free"

[backends.openrouter.headers]
X-Title = "omni"
```

The credential is **named, never written**. omni reads `api_key_env` from the
environment at launch, and a config file containing a credential-shaped value
is refused outright. `api_key_env` may be omitted only for a loopback
endpoint that wants no auth.

### omni routes; it does not translate

`api_style` must match the agent's own wire format. A mismatch is rejected at
load rather than forwarded and hoped over — omni will not send an
Anthropic-shaped body to an OpenAI-shaped endpoint. Point `base_url` at a
translating gateway if you need one.

### The credential boundary

Before a request leaves for a backend, the agent's credential headers —
`Authorization` and `x-api-key` — are replaced with the backend's own. Claude
Code authenticates with an Anthropic OAuth bearer token; forwarding that to an
arbitrary `base_url` would hand a live credential to whatever host that is,
and `Authorization` has to be rewritten regardless, since that is where the
backend's key goes.

The set is exactly what the recorder redacts to disk — `Authorization`,
`x-api-key`, and anything ending in `-api-key` — from one shared predicate,
so the headers omni refuses to forward can never drift from the ones it
treats as secret.

Those are the **only** headers omni removes. `anthropic-version`,
`anthropic-beta` and its feature gates, the `x-stainless-*` client telemetry
and everything else are forwarded verbatim — omni does not decide on the
agent's behalf which headers a backend wants. A backend that rejects one says
so, and a rejection you can see beats a header omni silently ate.

That decision is made **by host, not by name**. Credentials are forwarded only
when the backend resolves to the same host the agent would have reached
anyway — which is what makes an explicitly declared `[backends.anthropic]`
targetable, and what stops a backend *called* `anthropic` but pointed
elsewhere from talking omni into leaking a token there.

## Why it belongs in a proxy

Agents pin their model names. They validate them client-side, gate features
on them, and ship new ones on their own schedule. There is usually a
configuration knob, and it usually does not reach the case you care about.

A proxy does not have this problem. The agent sends a name it approves of;
the substitution happens on the wire, after every client-side check has
already passed.

## The rules routing has to obey

### Deterministic given the request

The mapping from a request to a model must be a pure function of that
request and your configuration. Never of the world — not load, not latency,
not a random split, not the time of day.

This is not a stylistic preference. Providers cache prompt prefixes, and the
cache is keyed on the model. A request that lands on a different model than
the previous one in the same conversation invalidates every cache tier for
that conversation: tools, system prompt, and message history. The next
request re-pays for the entire prefix.

A "smart" router that sends 20% of requests to a cheaper model can therefore
cost more than no router at all, and will do so unpredictably enough that
you will not immediately know why.

### Sticky for a session

For the same reason, a model chosen for a conversation stays chosen. Because
`match` runs against the requested model and nothing else, the same request
always resolves to the same rule — there is no state, no clock, and no
counter for a decision to drift on.

### Loud when it cannot apply

Model rewriting requires decoding the request body, which means omni must
model the provider's wire format. Today that means the Anthropic Messages
API and nothing else.

Configuring `[[route]]` rules for an agent whose wire format omni cannot
decode is an error that names the limitation, at config-check time and again
before launch:

```
omni: cannot apply --model-map for agent "codex"
  model rewriting is Anthropic-only in this version.
  codex sessions are recorded but not rewritten.
```

Silently ignoring the rules would be the worst outcome: you would believe you
were routing, and you would not be. The same applies to a missing credential
— a rule targeting a backend whose `api_key_env` is unset refuses the launch
rather than failing on the first matching request.

## Watching the cost

Every recorded session's `meta.json` totals four token counters, and
`cache_read_input_tokens_total` is the one that answers "is this costing me
money I did not expect?"

```json
"summary": {
  "input_tokens_total": 41233,
  "output_tokens_total": 8890,
  "cache_creation_input_tokens_total": 21044,
  "cache_read_input_tokens_total": 388102
}
```

Cache reads should dominate in a long session. If they collapse between a
recorded baseline and a routed session, something in the request prefix is
changing — that is the canary, and it is recorded whether or not routing is
on, precisely so the comparison is available.

## The adapter

Models differ in what they accept: context windows, tool-use shapes,
extended-thinking parameters, image support. A request that is valid for the
model the agent chose is not automatically valid for the model you routed it
to.

The planned answer is a capability adapter that runs after routing, with a
configured policy for what to do when a request cannot be faithfully
represented on the target model — refuse the request, or warn and send a
degraded one. Refusing is the right default: a silently downgraded request
produces a worse answer with no indication that anything happened.

There is no config for this yet, deliberately: a policy key for an adapter
that does not exist would only promise behaviour omni cannot deliver. The
keys arrive with the adapter.

Unlike routing, the adapter is designed and configurable but not yet
implemented.
