---
title: Environment variables
summary: How OMNI_* variables map onto config keys, the __ nesting rule,
  per-agent scoping, and the two keys the environment cannot set.
last_updated: 2026-08-31
related:
  - ../configuration/configuration-file/
  - ../reference/config-schema/
---

# Environment variables

Every scalar config key can be set from the environment. This is layer 5 of
[seven](../configuration-file/#precedence) — above every file, below CLI
flags — which makes it the right tool for one shell, one CI job, or one
`direnv` block.

```sh
OMNI_MODE=off omni claude
```

## The mapping

Take the dotted config path, uppercase it, replace each dot with `__`, and
prefix `OMNI_`.

| Config key | Variable |
|---|---|
| `mode` | `OMNI_MODE` |
| `record.enabled` | `OMNI_RECORD__ENABLED` |
| `redact` | `OMNI_REDACT` |
| `binary` (agent-scoped) | `OMNI_AGENTS__CLAUDE__BINARY` |
| `upstream` (agent-scoped) | `OMNI_AGENTS__CLAUDE__UPSTREAM` |

A double underscore is the separator; a single underscore is part of a key's
own name. No key in the current schema has an underscore in its own name, so
in practice `__` separates the `AGENTS` prefix from an agent name and its
key, and `RECORD` from its sub-key.

## Scoping to one agent

Prefix `AGENTS__<NAME>__` to apply a setting to a single agent, exactly as
`[agents.<name>]` does in `omni.conf`:

```sh
OMNI_AGENTS__CLAUDE__MODE=route     # only affects `omni claude`
OMNI_AGENTS__CODEX__RECORD__ENABLED=false
```

The agent name is matched case-insensitively. A variable scoped to a
different agent than the one being launched is ignored — not a warning, just
not applicable.

## What the environment cannot set

`route` is an ordered list and `env` is a map of your own keys, not the
schema's. Neither has a sensible flattening into environment-variable names —
a list needs indices and model names carry characters a variable name cannot
hold unambiguously — so omni does not try:

```sh
OMNI_ROUTE__CLAUDE_OPUS_5=claude-sonnet-5     # does not work
```

```
omni: route.claude_opus_5: environment variable
  OMNI_ROUTE__CLAUDE_OPUS_5 does not map to a known config key
  "route.claude_opus_5" (route and env cannot be set via environment
  variables) ($OMNI_ROUTE__CLAUDE_OPUS_5)
```

Use `--model-map from=to` on the command line for a one-off rewrite — it
becomes a rule ahead of the configured ones — or a `[[route]]` list in a
config file for a durable one.

The same warning appears for any `OMNI_*` variable that does not name a real
key. A typo'd variable that silently did nothing would be worse: your config
would be inert and you would have no way to tell.

## OMNI_HOME

`OMNI_HOME` is not a config key — it relocates the entire tree that config
is read from:

```sh
OMNI_HOME=/tmp/omni-scratch omni init
OMNI_HOME=/tmp/omni-scratch omni claude
```

Everything moves with it: `omni.conf`, `credentials`, `profiles.d/` and
`sessions/`. This is the supported way to keep omni's state somewhere other
than `~/.omni` — for a throwaway experiment, a second identity, or a test
harness that must not touch your real sessions. Note that the credentials
file moves too, so a relocated home starts with no stored keys.

Because it decides where config comes from, `OMNI_HOME` is never itself
treated as a config variable.

## Provenance

`omni config show` names the exact variable that set each value, so an
inherited setting from a shell profile or a CI environment is one command
away from being explained:

```
mode              "off"             $OMNI_MODE
record.enabled    true              $OMNI_RECORD__ENABLED
redact            true              ~/.omni/omni.conf:28
```
