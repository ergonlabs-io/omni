---
title: Model routing
summary: What model_map is for, why routing must be deterministic and
  session-sticky, and exactly how much of it exists today.
last_updated: 2026-08-31
related:
  - ../interception/how-it-works/
  - ../configuration/configuration-file/
  - ../sessions/recorded-sessions/
---

# Model routing

Routing rewrites the model name on the wire: the agent asks for one model,
omni forwards a request for another.

```toml
# ~/.omni/agents/claude.conf
mode = "route"

[model_map]
"claude-opus-5" = "claude-sonnet-5"
```

```sh
omni --mode route --model-map claude-opus-5=claude-sonnet-5 claude
```

:::caution[Not yet implemented]
`mode = "route"` and `model_map` are parsed, validated, and reported by
`omni config show` — but the rewrite itself is not wired in. A `route`
session today records exactly as a `record` session does, and forwards the
model the agent asked for. This page documents the design and the rules the
implementation has to satisfy; it will say otherwise when the behavior
lands.
:::

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

For the same reason, a model chosen for a conversation stays chosen. Routing
decisions are made once and held, not re-evaluated per request.

### Loud when it cannot apply

Model rewriting requires decoding the request body, which means omni must
model the provider's wire format. Today that means the Anthropic Messages
API and nothing else.

Configuring `model_map` for an agent whose wire format omni cannot decode is
an error that names the limitation, at config-check time and again before
launch:

```
omni: cannot apply --model-map for agent "codex"
  model rewriting is Anthropic-only in this version.
  codex sessions are recorded but not rewritten.
```

Silently ignoring the map would be the worst outcome: you would believe you
were routing, and you would not be.

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
represented on the target model:

```toml
[defaults.adapt]
on_unrepresentable = "error"   # error | warn
report_changes     = true
```

`error` refuses the request rather than sending a quietly degraded version of
it. That is the default, and the right one: a silently downgraded request
produces a worse answer with no indication that anything happened.

Like routing, the adapter is designed and configurable but not yet
implemented.
