---
title: Recorded sessions
summary: The on-disk format of a recorded session, what meta.json summarizes,
  and how recording stays out of the way of the traffic it captures.
last_updated: 2026-08-31
related:
  - ../sessions/redaction/
  - ../interception/how-it-works/
  - ../configuration/configuration-file/
---

# Recorded sessions

Recording is on by default. Every session writes one directory under
`~/.omni/sessions`, named for when it started and which agent ran:

```
~/.omni/sessions/2026-08-31T09-14-02-claude/
├── meta.json
├── 001.request.headers.json
├── 001.request.json
├── 001.response.headers.json
├── 001.response.sse
├── 002.request.headers.json
└── ...
```

Two sessions started in the same second get a numeric suffix rather than
sharing a directory.

## One exchange, four files

Exchanges are numbered in the order the proxy saw them.

| File | Contents |
|---|---|
| `NNN.request.headers.json` | Method, URL, and headers — credential headers redacted. |
| `NNN.request.json` | The request body, byte for byte. |
| `NNN.response.headers.json` | Status code and response headers, redacted the same way. |
| `NNN.response.sse` or `.json` | The response body. `.sse` when the response was a stream, `.json` otherwise. |

Bodies are stored verbatim, not reformatted. What is on disk is what crossed
the wire, which is the only version worth keeping — a prettified copy answers
a different question than the one you will be asking.

The extension is chosen from the response's `Content-Type`, so streaming and
non-streaming responses are distinguishable without opening them.

## meta.json

```json
{
  "agent": "claude",
  "omni_version": "0.1.0",
  "redacted": true,
  "start_time": "2026-08-31T09:14:02.114Z",
  "end_time": "2026-08-31T09:31:48.007Z",
  "exchanges": 27,
  "summary": {
    "input_tokens_total": 41233,
    "output_tokens_total": 8890,
    "cache_creation_input_tokens_total": 21044,
    "cache_read_input_tokens_total": 388102
  }
}
```

`redacted` records whether credential redaction was on for this capture, so a
session's provenance travels with it — you never have to remember which
setting was in effect when it was made. An `agent_version` field is reserved
for the agent's own version string and is omitted until omni learns it.

An initial `meta.json` is written when the session starts and finalized on
exit, so a session that ends in a crash still leaves a readable, if
incomplete, record.

### The numbers that matter

Token totals are parsed out of response bodies as they stream past, for both
SSE and non-streaming responses.

`cache_read_input_tokens_total` is the one to watch. Providers cache prompt
prefixes, and cache reads are dramatically cheaper than fresh input. In a
long session, cache reads should dominate. When that number collapses,
something is changing the request prefix between turns, and the session is
costing more than it should — which is precisely the failure a tool that sits
in the middle of your requests could introduce. Recording it unconditionally
means the before-and-after comparison is always available.

## Recording never gets in the way

The recorder runs beside the exchange, never in front of it:

- Response bytes go to the client **first**, then to the recorder.
- Writing happens on a background goroutine behind a buffered channel.
- If the disk cannot keep up, frames are dropped from the capture — with a
  warning — rather than applying backpressure to the response.
- If the recorder cannot be created at all, the session runs without
  recording and says so on stderr.

The rule is that a recording problem is never your outage. A capture with a
gap in it is a bad outcome; a session that stalls because a log file could
not be written is a much worse one.

## Turning it off

```sh
omni --mode off claude              # this run only
```

```toml
# ~/.omni/omni.conf
[defaults.record]
enabled = false
```

`mode = "off"` disables recording for the session; the proxy still runs and
still forwards. Per-agent and per-project overrides work the same way — see
[Configuration file](../../configuration/configuration-file/).

:::note[Phase 0]
`record.bodies` and `record.retention` are accepted and validated, but not
yet applied: bodies are always captured, and old sessions are not pruned
automatically. The `omni sessions` subcommand, which will list and prune
them, is not implemented — for now, `~/.omni/sessions` is an ordinary
directory and `rm -rf` is the prune command.
:::

## What a session contains

A recorded session holds your prompts, the model's replies, and whatever
source code, file contents, and command output the agent sent along with
them. Credentials are redacted from headers; nothing else about the content
is filtered.

Directories are created `0700` and files `0600` — readable by you and not by
other users on the machine. Treat the directory with the same care you would
treat the repository it came from, and see [Redaction](../redaction/) for
exactly what is and is not scrubbed.
