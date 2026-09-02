---
title: Per-project config
summary: What ./.omni.conf may set, why the allowlist is short, and why omni
  never reads config from parent directories.
last_updated: 2026-08-31
related:
  - ../configuration/configuration-file/
  - ../configuration/environment-variables/
  - ../reference/config-schema/
---

# Per-project config

A repository can carry its own omni settings in a file named `.omni.conf` in
its root:

```toml
# ./.omni.conf — checked into the repo
mode = "off"

[[route]]
match = "claude-opus-*"
model = "claude-sonnet-5"
```

This is layer 4 of [six](../configuration-file/#precedence): it overrides
your global and per-agent files, and is overridden by `OMNI_*` variables and
CLI flags.

## The allowlist

A project file may set exactly two things:

| Key | Effect |
|---|---|
| `mode` | `off` or `record` for work in this repo. |
| `route` | Routing rules — but only ones that rename a model. |

Everything else in the file is **ignored and reported as a warning**, by
name, with the file and line that set it:

```
omni: binary: "binary" is not permitted in project config
  (./.omni.conf may only set mode, route) — ignored
  (./.omni.conf:4)
```

A stray key is a mistake, not an attack, so it warns rather than failing the
launch. But it never takes effect.

## Why the list is that short

You `cd` into repositories you did not write. A project config is untrusted
input, and the keys it must never reach are the ones that would make
cloning a repository dangerous:

- **`binary`** — arbitrary code execution. A repo that could point omni's
  `claude` at `./tools/claude` would run its own program the moment you typed
  `omni claude`.
- **`upstream`** — silent exfiltration. Your API traffic, including your
  prompts and your source, redirected to someone else's endpoint.
- **`redact`** — a repo could turn off credential redaction and have your
  API key written to disk in plaintext.
- **`record.enabled`** — a repo could switch recording on and have your
  prompts, and its own source, written to `~/.omni/sessions` without you
  asking. Deciding to capture your work is your call, not the repository's.

The implementation matches the argument. The project layer does not decode
into a struct and then check the result; it walks the raw document and only
ever assigns a field for a key it has already allowed. A key added to the
schema later cannot leak into project scope just because a struct tag
happens to match it.

## The working directory only

omni reads `.omni.conf` from the current working directory. It never walks
up to parent directories.

Ancestor-walking config is a footgun on any tool, and a serious one on a
tool that decides which model serves your requests and whether your traffic
is recorded. You should be able to answer "what will omni do here?" by
looking at one directory, not by auditing every ancestor of it up to `/`.

The cost is that a monorepo needs a file per working directory, or none at
all. That is the intended trade.

## Seeing what it did

```sh
omni config check          # every warning, including ignored project keys
omni config show           # each effective value, and the layer that set it
```

`config show` labels a value that came from this layer with the file and
line, so a surprising setting resolves in one command.

## A project may rename, but not reroute

A `[[route]]` rule here may set `model`, but never `backend`:

```
omni: route: project config may not route to a backend ("openrouter")
  — ./.omni.conf can rename a model but not change its destination;
  move this to ~/.omni/omni.conf if you meant it (./.omni.conf:3)
```

Renaming a model within the agent's own provider is a local preference a
repository can reasonably express. Choosing which third party receives your
prompts, and bills you for them, is not — and the backend it named would be
one *you* declared globally, which makes the escalation quiet rather than
obvious. A project file cannot declare a `[backends.*]` table either.
