---
title: Config schema
summary: Every configuration key — type, default, where it may be set, its
  environment variable, and whether it is applied yet.
last_updated: 2026-08-31
related:
  - ../configuration/configuration-file/
  - ../configuration/environment-variables/
  - ../reference/cli/
---

# Config schema

Every key omni recognizes. Anything not on this list is an unknown key: it is
reported, with a suggestion if it is close to a real one, and ignored.

The **Scope** column says where a key may be set:

- **global** — `~/.omni/omni.conf` under `[defaults]`
- **agent** — `[agents.<name>]` in `omni.conf`
- **project** — `./.omni.conf` (a deliberately short allowlist; see
  [Per-project config](../../configuration/per-project-config/))

## Top level

| Key | Type | Default | Scope | Environment |
|---|---|---|---|---|
| `mode` | `off` \| `record` | `record` | global, agent, project | `OMNI_MODE` |
| `record.enabled` | bool | `false` | global, agent | `OMNI_RECORD__ENABLED` |
| `redact` | bool | `true` | global, agent | `OMNI_REDACT` |
| `binary` | string | agent's own | agent | `OMNI_AGENTS__<NAME>__BINARY` |
| `upstream` | string | agent's own | agent | `OMNI_AGENTS__<NAME>__UPSTREAM` |

That is every key. A setting exists only if omni does something with it.

`mode`:

- `off` — forward only; no rule is applied and nothing can be recorded.
- `record` — intercept: apply any `[[route]]` rules, and allow recording.

There is no `route` mode. Routing is on whenever you have written rules —
a rule you wrote is a rule you meant. `mode = "route"` is still accepted and
treated as `record`, so an existing config keeps working.

`record.enabled` is what actually writes a session to `~/.omni/sessions`, and
it is **off by default**. It is deliberately separate from `mode`: routing
leaves no trace, but a recording holds the prompts your agent sent — source
code and secrets from your working directory included — so it is opt-in on
its own. Recording needs both: `mode` not `off`, and `record.enabled` true.
`omni --record <agent>` sets it for one run.

`redact` strips `Authorization`, `x-api-key` and `*-api-key` headers from
recorded traffic. It does **not** sanitize bodies — see
[Redaction](../../sessions/redaction/).

## `[backends.<name>]`

Global only. A backend is a destination a routing rule can target.

| Key | Type | Default | Notes |
|---|---|---|---|
| `base_url` | string | required | `https`, or `http` on loopback only. |
| `api_key_env` | string | — | Name of the variable holding the credential. |
| `api_style` | `anthropic` \| `openai` | `anthropic` | Must match the agent's. |
| `model` | string | — | What to ask this backend for, absent a rule's own. |
| `headers` | table | — | Extra headers on every request to this backend. |

```toml
[backends.openrouter]
base_url    = "https://openrouter.ai/api"
api_key_env = "OPENROUTER_API_KEY"
api_style   = "anthropic"
model       = "minimax/minimax-m3:free"

[backends.openrouter.headers]
X-Title = "omni"
```

`api_key_env` names a variable; it is never the key itself, and a
credential-shaped value anywhere in config is refused at load. The variable
is resolved from omni's own environment first and from `~/.omni/credentials`
second — see [API keys and
credentials](../../configuration/credentials/). It may be omitted only for a
loopback endpoint, or for a backend that resolves to the agent's own
upstream — a remote backend with no credential is an error, because omni
strips the agent's own before forwarding.

## `[[route]]`

An ordered list of rules, first match wins, belonging to an agent. Not
settable from the environment.

| Key | Type | Notes |
|---|---|---|
| `match` | string | Required. Glob against the model the agent asked for. |
| `backend` | string | A declared backend, or omit to keep the agent's upstream. |
| `model` | string | Replaces the model. Omit to use the backend's, or leave it unchanged. |

```toml
[[agents.claude.route]]
match   = "claude-haiku-4-5*"
backend = "openrouter"

[[agents.claude.route]]
match = "claude-opus-*"
model = "claude-sonnet-5"
```

Glob syntax is `*` (any run, including `/` and `:`) and `?` (one character);
everything else is literal. A rule must set `backend`, `model`, or both.

In a repo-local `./.omni.conf` these are written `[[route]]`, unwrapped. A
layer declaring any rules **replaces** the list rather than appending — rules
are ordered, and there is no predictable order between two files' lists.

A project `./.omni.conf` may write rules, but only ones that rename a model;
a rule naming a `backend` is rejected.

## `[env]`

A table of user-chosen keys, so it cannot be set from the environment.

```toml
[env]
# extra environment for the child process
ANTHROPIC_LOG = "debug"
```

`env` belongs to an agent, not to `[defaults]`. It may not be set per
project, and never overrides omni's own steering variables.

## What is applied today

Every key in this schema is read by code that acts on it — that is the point
of the schema being this short.

| Key | Status |
|---|---|
| `mode` | Applied. `off` disables recording and routing. |
| `record.enabled` | Applied. Off by default; also requires `mode` ≠ `off`. |
| `redact` | Applied. |
| `binary`, `upstream` | Applied. |
| `route`, `backends.*` | Applied. |
| `env` | Applied. |

## Validation

One category is fatal: omni refuses to load a configuration containing it,
rather than warning and continuing.

- **A credential-shaped value, anywhere.** Any value matching an API-key or
  bearer-token shape is rejected wherever it appears — config files are not a
  place for secrets, and one written there tends to end up in a repository.

The proxy's loopback-only bind is not a config concern at all — omni picks
the address and `internal/proxy` enforces it.

Everything else — an unknown key, an invalid enum, a disallowed project key,
a `[[route]]` on an agent that cannot be rewritten —
is collected and reported. Errors fail `omni config check` and abort a launch
before the agent starts; warnings do not.

```sh
omni config check --agent claude
omni config show  --agent claude
```
