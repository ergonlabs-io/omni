---
title: Codex
summary: What omni can and cannot do for Codex today, and which parts of its
  profile are still unconfirmed.
last_updated: 2026-08-31
related:
  - ../agents/claude-code/
  - ../interception/all-traffic/
  - ../agents/custom-profiles/
---

# Codex

```sh
omni codex
```

Codex is recorded, not rewritten. Its profile is deliberately conservative:
where omni does not yet know something for certain, the profile says so by
refusing the feature rather than guessing.

## Profile

| | |
|---|---|
| Names | `codex` |
| Binary | `codex`, resolved on `PATH` |
| Steering variable | `OPENAI_BASE_URL` — **unconfirmed** |
| Upstream | `https://api.openai.com` |
| Wire format | OpenAI Chat Completions |
| Model rewriting | not supported |
| All-traffic trust | none known — `--all-traffic` is refused |

## Model rewriting is unavailable

Rewriting a model name means decoding the request body, and omni models the
Anthropic Messages API only in this version. `model_map` or `--model-map` on
Codex is an error, not a silent no-op:

```
omni: cannot apply --model-map for agent "codex"
  model rewriting is Anthropic-only in this version.
  codex sessions are recorded but not rewritten.
```

Recording works normally: requests, responses, headers, and token usage are
all captured, whatever the wire format, because recording operates on bytes.

## All-traffic is refused

Codex is a Rust program. If it uses rustls with bundled roots, it ignores
every CA environment variable that exists, and no amount of configuration
makes full MITM possible. Until that is confirmed one way or the other, its
profile carries no trust mechanism, and `--all-traffic` fails with a message
saying exactly that.

Its LLM traffic is still intercepted without the flag.

## Unconfirmed values

`OPENAI_BASE_URL` is the expected steering variable but has not been verified
against real captured traffic. If Codex traffic is not appearing in your
recorded sessions, that is the first thing to check — and a bug worth
reporting.

Confirming these values is exactly what recording is for: run a session, look
at what was captured, and the answer is on disk. See
[Recorded sessions](../../sessions/recorded-sessions/).
