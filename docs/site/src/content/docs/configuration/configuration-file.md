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
| 3 | `~/.omni/omni.conf` `[agents.X]` | per-agent, inline |
| 4 | `~/.omni/agents/X.conf` | per-agent drop-in |
| 5 | `./.omni.conf` | per-project |
| 6 | `OMNI_*` environment variables | per-invocation |
| 7 | CLI flags | highest |

Layers 3 and 4 both exist on purpose. `[agents.claude]` in `omni.conf` is for
one-liners you want beside your global settings; `agents/claude.conf` is for
anything substantial, and wins — the same way `conf.d` works everywhere else
in Unix.

Layer 5 is read from the working directory **only**. omni does not walk up
parent directories: ancestor-walking config is a footgun on a tool that
rewrites which model serves your requests, and you should be able to tell
what omni will do by looking at one directory. It may also set only three
keys — see [Per-project config](../per-project-config/).

## Global keys

```toml
[defaults]
# off | record | route
#   off     passthrough, no proxy involvement beyond forwarding
#   record  capture all traffic to ~/.omni/sessions (default)
#   route   record + apply model_map and the capability adapter
mode = "record"

all_traffic = false            # Tier 2 full MITM; requires a CA

[defaults.record]
enabled   = true
redact    = true               # strip Authorization / x-api-key / *-api-key
bodies    = true               # request+response bodies, not just metadata
retention = "14d"              # prune sessions older than this on startup

[defaults.adapt]
on_unrepresentable = "error"   # error | warn
report_changes     = true      # log every mutation the adapter makes

[defaults.proxy]
listen       = "127.0.0.1:0"   # ephemeral. Loopback only — do not change.
idle_timeout = "10m"           # must exceed a plausible tool-loop duration
```

`listen` is loopback-only by design. omni holds live credentials while it
runs, so a non-loopback bind address is refused at startup rather than
warned about.

## Per-agent config

`~/.omni/agents/claude.conf` overrides the global file for `omni claude`:

```toml
mode = "route"

# binary = "/Users/me/.local/bin/claude"   # pin a version or a local build

[model_map]
# LHS is what the agent sends, RHS is what omni forwards.
"claude-opus-5" = "claude-sonnet-5"

[adapt]
on_unrepresentable = "error"

[record]
bodies = true

[env]
# Extra env for the child. Never overrides omni's own steering variables
# (ANTHROPIC_BASE_URL and friends) — those always win.
# ANTHROPIC_LOG = "debug"
```

That is an example of a file you might write. The one `omni init` generates
has the same shape with every key commented out.

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
OMNI_RECORD__REDACT=false
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
- `model_map` on an agent whose wire format omni cannot rewrite is an error
  naming the limitation, not a silent no-op.
- Durations and enum values are parsed and reported by key, with the file and
  line that set them.

Two problems are fatal — omni refuses to load at all rather than warning: a
`proxy.listen` that is not loopback, and any value anywhere in config that
looks like an API key or bearer token. Every key, its default, and whether it
is applied yet is listed in the
[config schema](../../reference/config-schema/).

When something is surprising, `omni config show` annotates each effective
value with the layer that set it. With seven layers, "why is omni doing
that?" is the question, and provenance is the whole answer.
