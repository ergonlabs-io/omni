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
- `route` — record, and apply `model_map` and the capability adapter.

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

## `[model_map]` and `[env]`

Both are tables of user-chosen keys, so neither can be set from the
environment.

```toml
[model_map]
# what the agent sends = what omni forwards
"claude-opus-5" = "claude-sonnet-5"

[env]
# extra environment for the child process
ANTHROPIC_LOG = "debug"
```

Both belong to an agent, not to `[defaults]`: put them in
`[agents.<name>]` or in `~/.omni/agents/<name>.conf`. `model_map` may also be
set per project; `env` may not, and never overrides omni's own steering
variables.

## What is applied today

The whole schema is parsed, validated, merged with provenance, and reported
by `omni config show`. Not all of it is acted on yet.

| Key | Status |
|---|---|
| `mode` | Applied. `off` disables recording; `route` currently behaves as `record`. |
| `binary`, `upstream` | Applied. |
| `record.enabled`, `record.redact` | Applied. |
| `proxy.listen` | Applied, and enforced. |
| `env` | Applied. |
| `all_traffic` | Validated per agent; no CA is generated yet, so it has no effect. |
| `model_map` | Validated; the rewrite is not implemented. |
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
disallowed project key, a `model_map` on an agent that cannot be rewritten —
is collected and reported. Errors fail `omni config check` and abort a launch
before the agent starts; warnings do not.

```sh
omni config check --agent claude
omni config show  --agent claude
```
