---
title: Custom profiles
summary: How to adjust a built-in agent today, and what adding a brand-new
  agent requires.
last_updated: 2026-08-31
related:
  - ../agents/claude-code/
  - ../configuration/configuration-file/
  - ../reference/config-schema/
---

# Custom profiles

A profile is what omni knows about an agent: the binary to run, the
environment variable that steers its traffic, its real upstream, the wire
format it speaks, and whether its runtime can be made to trust a CA.

## Adjusting a built-in agent

Two of those fields are configurable per agent, which covers most of what
people actually need:

```toml
# ~/.omni/omni.conf
[agents.claude]
binary   = "/opt/claude-canary/bin/claude"   # which executable to run
upstream = "https://gateway.internal"        # where omni forwards to
```

That is enough for a local build, a pinned version, a corporate gateway, or a
test server standing in for the provider. Verify with:

```sh
omni --dry-run claude
```

which prints the resolved binary, the injected environment, and every
effective config value with its source.

## Adding a new agent

:::note[Phase 0]
`omni init` creates `~/.omni/profiles.d/` and omni's error messages point at
it, but nothing reads it yet. Registering a new agent from a config file is
designed and not implemented; today it takes a code change.
:::

In the source, a profile is a struct literal — adding one is meant to be that
small:

```go
register(&Profile{
    Name:       "claude",
    Aliases:    []string{"claude-code", "cc"},
    Binary:     "claude",
    Desc:       "Claude Code",
    BaseURLEnv: "ANTHROPIC_BASE_URL",
    Upstream:   "https://api.anthropic.com",
    APIStyle:   StyleAnthropic,
    TrustEnv:   []string{"NODE_EXTRA_CA_CERTS"},
})
```

The fields that matter when you fill one in:

| Field | Meaning |
|---|---|
| `BaseURLEnv` | The variable that redirects the agent at omni. Empty means the agent cannot be steered by environment alone and needs a config shim. |
| `APIStyle` | `anthropic`, `openai`, or `passthrough`. Only `anthropic` can be rewritten; `passthrough` means "record the bytes, never decode them". |
| `TrustEnv` | Variables that make the runtime trust omni's CA. Empty means `--all-traffic` is refused for this agent. |

## Fill it in conservatively

Every unknown should be filled in with the value that makes omni refuse,
not the value that makes it try:

- Unsure about the wire format? `passthrough`. Sessions are still recorded;
  nothing is decoded or rewritten on a guess.
- Unsure whether the runtime honors a CA variable? Leave `TrustEnv` empty.
  `--all-traffic` then fails with a clear message instead of appearing to
  work while intercepting nothing.

The built-in Codex profile is written this way on purpose. An agent that
half-works loudly is far better than one that half-works silently — you are
running omni to find out what your agent is doing, and a profile that guesses
wrong tells you a confident lie.

## Reserved names

These always resolve as subcommands and can never be agent names:

```
init  config  ca  version  help  completions  sessions  run
```

If a name is ambiguous, `omni run <name>` always means "run the agent".
