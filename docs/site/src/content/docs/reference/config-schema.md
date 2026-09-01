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
- **agent** — `[agents.<name>]` in `omni.conf`, or `~/.omni/agents/<name>.conf`
- **project** — `./.omni.conf` (a deliberately short allowlist; see
  [Per-project config](../../configuration/per-project-config/))

## Top level

| Key | Type | Default | Scope | Environment |
|---|---|---|---|---|
| `mode` | `off` \| `record` \| `route` | `record` | global, agent, project | `OMNI_MODE` |
| `all_traffic` | bool | `false` | global | `OMNI_ALL_TRAFFIC` |
| `binary` | string | agent's own | agent | `OMNI_AGENTS__<NAME>__BINARY` |
| `upstream` | string | agent's own | agent | `OMNI_AGENTS__<NAME>__UPSTREAM` |

`mode`:

- `off` — forward only; nothing is recorded.
- `record` — capture every exchange to `~/.omni/sessions`.
- `route` — record, and apply the `[[route]]` rules and the capability adapter.

## `[record]`

| Key | Type | Default | Scope | Environment |
|---|---|---|---|---|
| `enabled` | bool | `true` | global, agent | `OMNI_RECORD__ENABLED` |
| `redact` | bool | `true` | global, agent | `OMNI_RECORD__REDACT` |
| `bodies` | bool | `true` | global, agent, project | `OMNI_RECORD__BODIES` |
| `retention` | duration | `14d` | global, agent | `OMNI_RECORD__RETENTION` |

Durations accept Go's syntax plus a `d` suffix for days: `14d`, `1d12h`,
`90m`.

## `[adapt]`

| Key | Type | Default | Scope | Environment |
|---|---|---|---|---|
| `on_unrepresentable` | `error` \| `warn` | `error` | global, agent | `OMNI_ADAPT__ON_UNREPRESENTABLE` |
| `report_changes` | bool | `true` | global, agent | `OMNI_ADAPT__REPORT_CHANGES` |

## `[proxy]`

Global only — one omni process has one proxy.

| Key | Type | Default | Scope | Environment |
|---|---|---|---|---|
| `listen` | string | `127.0.0.1:0` | global | `OMNI_PROXY__LISTEN` |
| `idle_timeout` | duration | `10m` | global | `OMNI_PROXY__IDLE_TIMEOUT` |

`listen` must resolve to loopback. `127.0.0.1`, `::1`, and `localhost` are
accepted; a bare `:8080`, a LAN address, or a hostname that cannot be
positively identified as loopback is refused at load.

## `[backends.<name>]`

Global only. A backend is a destination a routing rule can target.

| Key | Type | Default | Notes |
|---|---|---|---|
| `base_url` | string | required | `https`, or `http` on loopback only. |
| `api_key_env` | string | — | Name of the env var holding the credential. |
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
credential-shaped value anywhere in config is refused at load. It may be
omitted only for a loopback endpoint, or for a backend that resolves to the
agent's own upstream — a remote backend with no credential is an error,
because omni strips the agent's own before forwarding.

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

In a `~/.omni/agents/<name>.conf` drop-in these are written `[[route]]`,
unwrapped. A layer declaring any rules **replaces** the list rather than
appending — rules are ordered, and there is no predictable order between two
files' lists.

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

The whole schema is parsed, validated, merged with provenance, and reported
by `omni config show`. Not all of it is acted on yet.

| Key | Status |
|---|---|
| `mode` | Applied. `off` disables recording; `route` applies the rules. |
| `binary`, `upstream` | Applied. |
| `record.enabled`, `record.redact` | Applied. |
| `proxy.listen` | Applied, and enforced. |
| `env` | Applied. |
| `all_traffic` | Validated per agent; no CA is generated yet, so it has no effect. |
| `route`, `backends.*` | Applied. |
| `record.bodies` | Not applied — bodies are always captured. |
| `record.retention` | Not applied — sessions are not pruned automatically. |
| `adapt.*` | Not applied — there is no adapter yet. |
| `proxy.idle_timeout` | Not applied — the proxy uses a fixed idle timeout. |

## Validation

Two categories are fatal: omni refuses to load a configuration containing
them, rather than warning and continuing.

- **A non-loopback `proxy.listen`.** The proxy holds live credentials in
  flight and must not be reachable off-host.
- **A credential-shaped value, anywhere.** Any value matching an API-key or
  bearer-token shape is rejected wherever it appears — config files are not a
  place for secrets, and one written there tends to end up in a repository.

Everything else — an unknown key, a bad duration, an invalid enum, a
disallowed project key, a `[[route]]` on an agent that cannot be rewritten —
is collected and reported. Errors fail `omni config check` and abort a launch
before the agent starts; warnings do not.

```sh
omni config check --agent claude
omni config show  --agent claude
```
