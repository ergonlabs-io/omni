---
title: Configuration file
summary: The ~/.omni tree, omni.conf's TOML keys, the six precedence layers,
  and how to find out which layer set a value.
last_updated: 2026-08-31
related:
  - ../getting-started/installation/
  - ../reference/cli/
---

# Configuration

omni reads TOML. `omni init` writes a fully commented `~/.omni/omni.conf`,
which is the fastest way to see every key with its default.

```sh
omni config path            # print the home directory
omni config show            # effective config, annotated with provenance
omni config check           # validate without launching anything
```

## Precedence

Lowest to highest. Later wins per key — a deep merge, not a whole-file
replace.

| | Layer | Scope |
|---|---|---|
| 1 | Built-in defaults | compiled in |
| 2 | `~/.omni/omni.conf` `[defaults]` | global |
| 3 | `~/.omni/omni.conf` `[agents.X]` | per-agent |
| 4 | `./.omni.conf` | per-project |
| 5 | `OMNI_*` environment variables | per-invocation |
| 6 | CLI flags | highest |

`omni init` writes a single `omni.conf` holding everything, with per-agent
settings in `[agents.<name>]` tables. That is the only place a per-agent
setting lives.

One exception to the per-key deep merge: a `[[route]]` list does not merge
across layers. A layer that declares any rules replaces the list, because
rules are ordered and first-match-wins, and there is no order between two
files' lists a reader could predict.

Layer 4 is read from the working directory **only**. omni does not walk up
parent directories: ancestor-walking config is a footgun on a tool that
rewrites which model serves your requests, and you should be able to tell
what omni will do by looking at one directory. It may also set only three
keys — see [Per-project config](../per-project-config/).

## Global keys

```toml
[defaults]
# off | record
#   off     passthrough, no proxy involvement beyond forwarding
#   record  intercept: apply rules, and allow recording (default)
# [[route]] rules apply whenever they exist and mode is not "off".
mode = "record"

record.enabled = false         # write sessions to ~/.omni/sessions; off by default
redact = true                  # strip Authorization / x-api-key / *-api-key
```

Those three keys are the whole of `[defaults]`. omni deliberately has no key
for a feature it cannot perform yet.

`mode` and `record.enabled` are separate switches on purpose. Interception
and routing leave nothing behind; recording writes your prompts — and the
source code and secrets they carry — to disk. Making one imply the other
would mean either disabling routing for everyone or recording for everyone,
so recording opts in on its own (`omni --record <agent>` for a single run).

The proxy binds loopback on an ephemeral port and is not configurable. omni
holds live credentials while it runs, so a non-loopback bind is refused
outright rather than made a setting.

## Backends

Global only. A backend is a destination a routing rule can target; see
[Model routing](../../interception/model-routing/).

```toml
[backends.openrouter]
base_url    = "https://openrouter.ai/api"
api_key_env = "OPENROUTER_API_KEY"   # the variable's name, never the key
api_style   = "anthropic"
model       = "minimax/minimax-m3:free"
```

`api_key_env` names a variable. omni looks it up in its own environment and,
failing that, in `~/.omni/credentials` — a `0600` file that is the one place
a key may be written down. See
[API keys and credentials](../credentials/).

## Per-agent config

Per-agent settings go in an `[agents.<name>]` table:

```toml
[agents.claude]
mode = "record"

# binary = "/Users/me/.local/bin/claude"   # pin a version or a local build

# Routing rules: ordered, first match wins.
[[agents.claude.route]]
match   = "claude-haiku-4-5*"
backend = "openrouter"

[[agents.claude.route]]
match = "claude-opus-*"
model = "claude-sonnet-5"
```

A repo-local `./.omni.conf` writes the same settings flat, without the
`agents.claude` prefix — `mode` at the top level and rules as
`[[route]]` — though only a short allowlist of keys is honored there:

```toml
mode = "record"

[[route]]
match   = "claude-haiku-4-5*"
backend = "openrouter"

[env]
# Extra env for the child. Never overrides omni's own steering variables
# (ANTHROPIC_BASE_URL and friends) — those always win.
# ANTHROPIC_LOG = "debug"
```

That is an example of a file you might write. The `omni.conf` that `omni
init` generates has the same shape with every key commented out.

Model rewriting is sticky for a session rather than per-request, for
prompt-cache reasons: a model that changes mid-session throws away the cached
prefix and costs more than the routing saves. See
[Model routing](../../interception/model-routing/) — including how much of it
is implemented today.

:::note[The generated file overrides nothing]
The `claude.conf` you get from `omni init` is comments only, so `omni claude`
inherits every value from `omni.conf` until you uncomment something. That is
deliberate: `omni init` will not rewrite this file once it exists, so a
default shipped here would be one you keep forever. `omni --dry-run claude`
shows what is in effect and which layer set it.
:::

## Environment variables

Every key is settable as `OMNI_<PATH>`, with `__` for nesting:

```sh
OMNI_HOME=/tmp/omni-test
OMNI_MODE=route
OMNI_REDACT=false
OMNI_AGENTS__CLAUDE__MODE=record
```

`OMNI_HOME` is special: it relocates the whole tree, and is the supported way
to keep omni's state somewhere other than `~/.omni`. The full mapping rules,
including what the environment cannot set, are in
[Environment variables](../environment-variables/).

## Validation

`omni config check` validates semantics, not just syntax, and everything it
checks is also checked before a child process launches — failing after the
agent has started is much worse than failing in five milliseconds.

- Unknown keys are an error, with a suggestion for near-misses. Silently
  ignoring a typo'd key means your config does nothing and you cannot tell.
- A `[[route]]` list on an agent whose wire format omni cannot rewrite is an error
  naming the limitation, not a silent no-op.
- Durations and enum values are parsed and reported by key, with the file and
  line that set them.

Two problems are fatal — omni refuses to load at all, rather than warning:
any value anywhere in config that looks like an API key or bearer token, and
a `~/.omni/credentials` file whose permissions let anyone but you read it.
Every key, its default, and whether it is applied yet is listed in the
[config schema](../../reference/config-schema/).

When something is surprising, `omni config show` annotates each effective
value with the layer that set it. With six layers, "why is omni doing
that?" is the question, and provenance is the whole answer.
