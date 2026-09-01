---
title: CLI reference
summary: omni's flags, subcommands, argument-passthrough rule, and exit
  codes, verified against the argument parser.
last_updated: 2026-08-31
related:
  - ../getting-started/installation/
  - ../configuration/configuration-file/
---

# CLI reference

```
omni [flags] <agent> [agent-args...]
omni <subcommand> [flags]
```

Run `omni --help` for the authoritative list; it also reports which agents
were found on your `PATH`.

## The passthrough rule

Everything after the agent name belongs to the agent, verbatim. omni's own
flags come before it. `--` is accepted as an explicit terminator.

```sh
omni --mode route claude --some-claude-flag   # --mode is omni's, the rest is Claude's
omni claude --help                            # Claude Code's help
omni --help                                   # omni's help
```

An unknown flag before the agent name is an error rather than a
pass-through — a tool that silently swallows the agent's arguments is worse
than one that stops.

Reserved names always resolve as subcommands and can never be agents. If a
name is ambiguous, `omni run <agent>` is the unambiguous form.

## Flags

| Flag | Value | Meaning |
|---|---|---|
| `--mode` | `off\|record\|route` | Interception mode. Overrides config. |
| `--record-only` | | Shorthand for `--mode record`. |
| `--model-map` | `from=to` | One-off model rewrite. Repeatable. |
| `--all-traffic` | | Full MITM of all traffic, not just the LLM API. Requires a CA, and fails loudly on agents with no confirmed trust mechanism. |
| `--dry-run` | | Print what would happen; launch nothing. |
| `-v`, `--verbose` | | Diagnostics to stderr. Repeatable. |
| `-q`, `--quiet` | | Suppress warnings. |
| `--no-color` | | Disable color. `NO_COLOR` is also honored. |
| `--version` | | Version, commit, and build date. |
| `-h`, `--help` | | omni's help. |

`--model-map` on an agent whose wire format omni cannot decode is an error
naming the limitation. Model rewriting is Anthropic-only in this version;
other agents are recorded but not rewritten.

## Subcommands

| Subcommand | What it does |
|---|---|
| `init` | Create or repair `~/.omni`. Idempotent; never overwrites an existing file. |
| `config path` | Print the home directory. |
| `config show [--agent X]` | Effective configuration, annotated with the layer that set each value. |
| `config check [--agent X]` | Validate configuration and exit. Binds nothing, launches nothing. |
| `run <agent>` | Run an agent — the unambiguous form of `omni <agent>`. |
| `ca` | Manage the Tier 2 certificate authority. Not yet implemented. |
| `sessions` | List and prune recorded sessions. Not yet implemented. |
| `completions` | Generate shell completions. Not yet implemented. |
| `version`, `help` | Same as `--version` and `--help`. |

## Exit codes

**When the agent ran, omni exits with the agent's exit code.** A script
wrapping `omni claude` sees exactly what bare `claude` would give it.

When omni itself fails before or instead of the agent, it uses `sysexits.h`
codes, which are unlikely to collide with an agent's own:

| Code | Name | Cause |
|---|---|---|
| `64` | `EX_USAGE` | Bad flags, unknown agent, malformed invocation. |
| `66` | `EX_NOINPUT` | A referenced config file is unreadable. |
| `69` | `EX_UNAVAILABLE` | Agent binary not on `PATH`, or the proxy could not bind. |
| `70` | `EX_SOFTWARE` | Internal error — a bug in omni. |
| `78` | `EX_CONFIG` | Configuration invalid, syntactically or semantically. |

Collisions are still possible in principle. The disambiguator is that every
omni-originated failure prints to stderr prefixed `omni: `, and the agent's
output never is.

## Output discipline

omni writes nothing to stdout that the agent did not write. Diagnostics,
warnings, and errors go to stderr; there is no banner. `omni <agent>
2>/dev/null` gives you byte-identical stdout to running the agent directly.

That holds on both sides of the process. The launcher copies PTY bytes
through without interpreting them, and the proxy's tests assert that request
and response bodies arrive byte-identical, streaming frames included. If you
see something on stdout that the agent did not print, that is a bug worth
reporting.

Next: [Configuration schema](../config-schema/) for every key the flags
override, or [Troubleshooting](../troubleshooting/) when an invocation does
something you did not ask for.
