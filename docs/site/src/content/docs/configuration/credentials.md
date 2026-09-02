---
title: API keys and credentials
summary: Why keys never go in omni.conf, the ~/.omni/credentials file, its
  0600 requirement, and how it differs from exporting a variable.
last_updated: 2026-09-02
related:
  - ../configuration/configuration-file/
  - ../interception/model-routing/
  - ../reference/config-schema/
---

# API keys and credentials

Routing sends your requests to a backend that is not your agent's provider,
so omni has to authenticate to that backend itself. It needs a key — and
there is exactly one place you may put one, which is not `omni.conf`.

## Keys never go in config

A backend names the *variable* holding its key, never the key:

```toml
[backends.openrouter]
base_url    = "https://openrouter.ai/api"
api_key_env = "OPENROUTER_API_KEY"   # a name, not a secret
```

Pasting the key itself anywhere in a config file is refused at load, not
warned about:

```
omni: config: refusing to load: backends.openrouter.api_key_env: value looks
  like a credential (API key or bearer token) — never put credentials in
  config; they come from the environment or the agent's own auth
  (~/.omni/omni.conf:47)
```

`omni.conf` is created world-readable, gets committed to dotfile repos, and
is the first thing people paste into a bug report. It is the wrong container
for a secret, and a rule that only warned would be a rule everyone ignored.

## Two places a key can come from

omni looks up `api_key_env` in two places, in this order:

1. **Its own environment** — whatever you exported in the shell that ran
   `omni`.
2. **`~/.omni/credentials`** — an optional file of `NAME=value` lines.

The environment wins, which makes exporting the variable the natural way to
override the stored key for one run:

```sh
OPENROUTER_API_KEY=sk-or-v1-other omni claude    # just this once
```

## The credentials file

Create it yourself — `omni init` does not, because an empty secrets file
teaches nothing:

```sh
printf 'OPENROUTER_API_KEY=sk-or-v1-...\n' > ~/.omni/credentials
chmod 600 ~/.omni/credentials
```

The format is deliberately boring. One `NAME=value` per line, `#` comments,
blank lines ignored:

```sh
# ~/.omni/credentials
OPENROUTER_API_KEY=sk-or-v1-...
export TOGETHER_API_KEY="tok-..."      # a leading `export` is accepted
GROQ_API_KEY='gsk-...'                 # so is one pair of quotes
```

`NAME` must be a valid environment variable name, and matching some
backend's `api_key_env` is the only thing that makes it useful. Nothing is
expanded and no escape sequences are interpreted — `$OTHER` and `\n` are
literal characters in the value, because a key has to survive verbatim.

### It must be mode 0600

A credentials file that anyone else can read is refused, not read:

```
omni: config: refusing to load: credentials: credentials file is 0644, not
  0600 — it holds API keys and must not be readable by anyone else; fix
  with: chmod 0600 /Users/you/.omni/credentials
```

Reading it anyway would leave the key exposed while teaching you that the
mode does not matter. Stricter is fine: `0400` is accepted.

## What the file is not

It is not a config layer, and the distinction is load-bearing:

- It is **never merged into your configuration**. `omni config show` will not
  print it — the output stays safe to paste into an issue.
- It is **never recorded**, in a session or anywhere else.
- It is **never put into omni's environment**, which means it is never
  inherited by the agent omni launches.

That last point is the substantive difference between the two sources. omni
hands the child agent its own environment, so a key you `export` in your
shell is visible to the agent; a key kept in the credentials file is used by
omni alone, to authenticate to the backend, and the agent never sees it. If
you would rather your coding agent could not read your OpenRouter key, that
is the reason to use the file.

## Checking what resolved

`omni config check` reports a backend whose key resolves from neither place,
naming both:

```
warning: backends.openrouter.api_key_env: $OPENROUTER_API_KEY is set neither
  in the environment nor in /Users/you/.omni/credentials; routes to backend
  "openrouter" will fail (~/.omni/omni.conf:47)
```

It is a warning there because a backend can be declared and never targeted.
Once a routing rule actually points at it, the same problem stops a launch
outright rather than letting the agent start and fail on its first matching
request.

Neither command ever prints a key, and no error message quotes a value read
from the credentials file — only names, line numbers, and the path.
