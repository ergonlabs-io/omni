---
title: Claude Code
summary: How omni launches Claude Code, what it intercepts, and which
  features are available for it.
last_updated: 2026-08-31
related:
  - ../agents/codex/
  - ../interception/how-it-works/
  - ../configuration/configuration-file/
---

# Claude Code

Claude Code is the reference agent: it is the one whose wire format omni can
decode, and the one where every feature is available first.

```sh
omni claude
omni cc                # alias
omni claude-code       # alias
```

## Profile

| | |
|---|---|
| Names | `claude`, `claude-code`, `cc` |
| Binary | `claude`, resolved on `PATH` |
| Steering variable | `ANTHROPIC_BASE_URL` |
| Upstream | `https://api.anthropic.com` |
| Wire format | Anthropic Messages API |
| Model rewriting | supported (the only agent where it is) |
| All-traffic trust | `NODE_EXTRA_CA_CERTS` — Claude Code is a Node program |

`omni --help` reports whether the binary was found and where, so "is it
installed" and "which one will run" are answerable without a second command.

## What gets intercepted

By default, requests to `https://api.anthropic.com` — every message
exchange, including streaming responses, captured as they cross the wire.
Anything else Claude Code talks to is untouched until
[all-traffic mode](../../interception/all-traffic/) lands.

Streaming is preserved exactly: each SSE event is flushed as it arrives, and
the response is recorded to a `.sse` file alongside the request that produced
it. The TUI must look the same through omni as without it.

## Pointing at a specific binary

If you have several installs — a released version and a local build, say —
pin one:

```toml
# ~/.omni/agents/claude.conf
binary = "/Users/me/.local/bin/claude"
```

`omni --dry-run claude` prints the binary it resolved, so the pin is
verifiable before you launch anything.

## Pointing at a different upstream

`upstream` overrides where omni forwards to, which is what a gateway, a
regional endpoint, or a test server needs:

```toml
# ~/.omni/agents/claude.conf
upstream = "https://my-gateway.internal"
```

This is a global or per-agent setting only. A project's `.omni.conf` may not
set it — a checkout that could redirect your API traffic is a checkout that
could exfiltrate your prompts. See
[Per-project config](../../configuration/per-project-config/).

## Extra environment

`[env]` injects variables into the child process:

```toml
# ~/.omni/agents/claude.conf
[env]
ANTHROPIC_LOG = "debug"
```

omni's own steering variables always win over `[env]`. Pointing
`ANTHROPIC_BASE_URL` somewhere else from config would mean omni had launched
an agent it cannot see, which is never what you want; if you want omni out of
the way, use `--mode off` or run `claude` directly.
