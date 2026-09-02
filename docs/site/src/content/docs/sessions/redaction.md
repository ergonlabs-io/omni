---
title: Redaction
summary: Which headers are scrubbed before anything reaches disk, what is
  deliberately not scrubbed, and how to turn redaction off if you must.
last_updated: 2026-08-31
related:
  - ../sessions/recorded-sessions/
  - ../configuration/configuration-file/
---

# Redaction

omni sits between an agent and its API, which means every request it records
carries live credentials. Those credentials never reach disk.

## What is redacted

Before a header set is written, any header carrying a credential has its
value replaced:

| Header | Matched |
|---|---|
| `Authorization` | exact, case-insensitive |
| `x-api-key` | exact, case-insensitive |
| `*-api-key` | any header ending in `-api-key`, e.g. `anthropic-api-key` |

```json
{
  "method": "POST",
  "url": "/v1/messages",
  "header": {
    "Content-Type": ["application/json"],
    "X-Api-Key": ["[REDACTED]"],
    "Anthropic-Version": ["2023-06-01"]
  }
}
```

The header **name** is kept. Which authentication shape an agent used —
`x-api-key` versus `Authorization: Bearer` — is exactly the kind of thing you
record a session to learn, and keeping the name costs nothing while keeping
the value costs everything.

Request and response headers are both redacted, on the same rules.

## Redaction is a positive opt-out

A recorder is redacting unless something explicitly turned it off. There is no
path through the code where forgetting a flag produces an unredacted capture;
the tests assert both the default and the opt-out, and one of them scans a
full captured corpus for credential-shaped strings.

The setting is recorded in each session's `meta.json` as `"redacted": true`
or `false`, so a capture always states what was done to it.

## What is not redacted

**Bodies are stored verbatim.** Prompts, model output, file contents, diffs,
command output, error messages — all of it, exactly as it crossed the wire.

This is deliberate. A recorded session exists to answer "what was actually
sent?", and a body that has been pattern-matched and partially rewritten
answers a different, less useful question — while still not being safe to
share, because no pattern catches everything.

So the honest posture is: bodies are unfiltered, and a session directory is
as sensitive as the repository it was recorded in. omni protects it with
permissions (`0700` directories, `0600` files) rather than with a filter that
would give you false confidence.

If a credential is pasted into a prompt, it is in the recording. Treat
sessions accordingly, and do not attach one to a bug report without reading
it first.

## Turning it off

```toml
# ~/.omni/omni.conf
[defaults]
redact = false
```

There are real reasons to want this — reproducing a signing bug, or
diagnosing an auth failure that depends on the exact token. It writes live
API keys to disk in plaintext, so do it in a scratch home and delete it after:

```sh
OMNI_HOME=/tmp/omni-debug omni init
OMNI_HOME=/tmp/omni-debug OMNI_REDACT=false omni claude
# ... reproduce ...
rm -rf /tmp/omni-debug
```

A project's `.omni.conf` cannot set `redact`. A repository you cloned must
never be able to turn off credential redaction on your machine — see
[Per-project config](../../configuration/per-project-config/).
