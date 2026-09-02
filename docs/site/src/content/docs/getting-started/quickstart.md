---
title: Quickstart
summary: A first session end to end — initialize the home directory, run an
  agent through the proxy, and read what was recorded.
last_updated: 2026-08-31
related:
  - ../getting-started/installation/
  - ../sessions/recorded-sessions/
  - ../reference/cli/
---

# Quickstart

This assumes omni is installed and at least one agent (`claude` or `codex`)
is on your `PATH`. See [Installation](../installation/) otherwise.

## 1. Initialize

```sh
omni init
```

Creates `~/.omni` with a fully commented `omni.conf` and an empty
`sessions/`. It is idempotent and never overwrites a file you
have edited, so run it again any time you want a missing piece restored.

```sh
omni config path      # where omni thinks its home is
omni --help           # which agents were found on PATH
```

## 2. Look before you launch

`--dry-run` prints the resolved binary, the environment omni would inject,
and every effective config value with the layer that set it. It launches
nothing and binds nothing.

```sh
omni --dry-run claude
```

```
would launch: /Users/me/.local/bin/claude
with env:
  ANTHROPIC_BASE_URL=http://127.0.0.1:<ephemeral>
config:
  mode                   "record"                 ~/.omni/omni.conf:15
  redact                 true                     ~/.omni/omni.conf:20
sessions -> /Users/me/.omni/sessions
```

:::note[Every value here comes from `omni.conf`]
`omni init` writes one file, and every key in it that would change your
agent's behavior is commented out — so a fresh install records and forwards,
and nothing else. Uncomment a key under `[agents.claude]` to override a
global default for `omni claude` only, and `--dry-run` to confirm what is
actually in effect, including which line set it. The commented
`[[agents.claude.route]]` block is
[model routing](../../interception/model-routing/).
:::

The `with env` block is the whole of Tier 1 interception: one base-URL
variable, scoped to this child process. Nothing is installed, nothing is
changed system-wide.

## 3. Run a session

```sh
omni claude
```

Claude Code starts exactly as it does unwrapped — same TUI, same keys, same
resize behavior — because it is running in a PTY that omni proxies byte for
byte. Everything after the agent name is the agent's:

```sh
omni claude --resume            # --resume goes to Claude Code
omni --mode record claude       # --mode is omni's
```

Add `-v` if you want to see omni's own diagnostics (the proxy address, the
session directory) on stderr. Without it, omni is silent.

## 4. Read the session

```sh
ls ~/.omni/sessions
# 2026-08-31T09-14-02-claude

cd ~/.omni/sessions/2026-08-31T09-14-02-claude
cat meta.json
```

```json
{
  "agent": "claude",
  "omni_version": "0.1.0",
  "redacted": true,
  "start_time": "2026-08-31T09:14:02Z",
  "end_time": "2026-08-31T09:31:48Z",
  "exchanges": 27,
  "summary": {
    "input_tokens_total": 41233,
    "output_tokens_total": 8890,
    "cache_creation_input_tokens_total": 21044,
    "cache_read_input_tokens_total": 388102
  }
}
```

Each exchange is four files, numbered in order:

```
001.request.headers.json    method, URL, headers (credentials redacted)
001.request.json            the request body, verbatim
001.response.headers.json   status and response headers
001.response.sse            the response body — .sse when streaming, .json otherwise
```

`cache_read_input_tokens_total` is the number to watch. It is the direct
measure of whether the provider's prompt cache is still being hit; a session
where it collapses is a session that got more expensive. See
[Recorded sessions](../../sessions/recorded-sessions/).

## 5. Turn it off

Recording is on by default. To stop it for one run:

```sh
omni --mode off claude
```

The proxy still runs and still forwards; nothing is written to disk. To
change the default, edit `mode` in `~/.omni/omni.conf` or
the `[agents.claude]` section of `~/.omni/omni.conf` — see
[Configuration file](../../configuration/configuration-file/).

## Where to go next

- [How interception works](../../interception/how-it-works/) — what sits
  between the agent and the API, and what it is allowed to do.
- [Recorded sessions](../../sessions/recorded-sessions/) — the on-disk
  format in full.
- [CLI reference](../../reference/cli/) — every flag, subcommand, and exit
  code.
