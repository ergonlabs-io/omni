---
title: Introduction
summary: What omni is, why interception happens in a proxy rather than in
  each agent's configuration, and what exists today.
last_updated: 2026-08-31
related:
  - ../getting-started/installation/
  - ../configuration/configuration-file/
---

# Introduction

omni runs a coding agent CLI — Claude Code, Codex, anything with a profile —
inside a PTY, with the agent's LLM API traffic pointed at a proxy omni
starts on loopback. The agent behaves exactly as it does unwrapped. omni sees
every request and response.

```sh
omni claude
```

Everything after the agent name is the agent's. `omni claude --help` prints
Claude Code's help; `omni --help` prints omni's. This rule does not bend —
scripts and muscle memory are built on it.

## Why a proxy

Agents pin their model names and validate them client-side, so configuration
overrides cannot reach the cases that matter. A proxy can: the agent emits a
name it approves of, and the substitution happens on the wire.

The same argument applies to everything else omni does. A wrapper that
reimplements each agent's config format breaks on every upstream release. One
proxy speaking the provider's API works across agents that share no code, and
keeps working when those agents change.

## What omni does not do

- **It does not install a CA into your system trust store.** Full-MITM mode
  scopes trust with environment variables, to the processes omni launches and
  nothing else.
- **It does not bind anything but loopback.** The proxy holds live
  credentials; a non-loopback bind is refused rather than warned about.
- **It does not read config from parent directories.** Project config comes
  from the working directory only, so what omni will do is visible in one
  place.

## Status

Phase 0 — recording and reconnaissance. The CLI, the `~/.omni` configuration
system, Tier 1 interception, and session recording are implemented. Model
routing, the capability adapter, all-traffic interception, and the session
management commands are designed but not yet wired in. Their pages exist and
say so at the top, because the constraints they have to satisfy are worth
knowing before you rely on them.

Next: [Installation](../installation/), then a first session in the
[Quickstart](../quickstart/). For what actually sits between the agent and
the API, see [How interception works](../../interception/how-it-works/).
