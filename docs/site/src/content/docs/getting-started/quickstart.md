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
  mode                   "record"                 ~/.omni/omni.conf:16
  record.enabled         false                    ~/.omni/omni.conf:23
  redact                 true                     ~/.omni/omni.conf:28
recording: off — enable with --record, or record.enabled in config
```

:::note[Every value here comes from `omni.conf`]
`omni init` writes one file, and every key in it that would change your
agent's behavior is commented out — so a fresh install forwards traffic and
nothing else, writing nothing to disk until you ask it to. Uncomment a key under `[agents.claude]` to override a
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
omni --record claude            # --record is omni's
```

Add `-v` if you want to see omni's own diagnostics (the proxy address, the
session directory) on stderr. Without it, omni is silent.

## 4. Record a session and read it

Nothing was written to disk in step 3: recording is off until you ask for it.
Run it again with `--record`, then look at what it left behind.

```sh
omni --record claude
```

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

Bodies are files, numbered in order; everything about an exchange lives in
one append-only index:

```
exchanges.jsonl             headers, status, and timing — two lines per exchange
001.request.json            the request body, verbatim
001.response.sse            the response body — .sse when streaming, .json otherwise
```

```sh
# what did the upstream actually return?
jq -r 'select(.type == "response") | "\(.seq) \(.status) \(.ttfb_ms)ms"' exchanges.jsonl
```

`cache_read_input_tokens_total` is the number to watch. It is the direct
measure of whether the provider's prompt cache is still being hit; a session
where it collapses is a session that got more expensive. See
[Recorded sessions](../../sessions/recorded-sessions/).

## 5. Turn it off

Recording is already off unless you pass `--record`, so the way to stop
writing a session is to leave the flag out. To make it the default for one
agent instead, set `record.enabled = true` under `[agents.claude]` in
`~/.omni/omni.conf`, and `--no-record` will still turn it off for a single
run.

To take omni out of the path entirely — no interception, no routing, pure
forwarding:

```sh
omni --mode off claude
```

See [Configuration file](../../configuration/configuration-file/).

## Where to go next

- [How interception works](../../interception/how-it-works/) — what sits
  between the agent and the API, and what it is allowed to do.
- [Recorded sessions](../../sessions/recorded-sessions/) — the on-disk
  format in full.
- [CLI reference](../../reference/cli/) — every flag, subcommand, and exit
  code.
