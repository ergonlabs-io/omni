---
title: All-traffic mode and the CA
summary: What --all-traffic would intercept beyond the LLM API, why trust is
  scoped with environment variables instead of your system store, and why it
  fails loudly on agents that cannot support it.
last_updated: 2026-08-31
related:
  - ../interception/how-it-works/
  - ../agents/claude-code/
  - ../agents/codex/
---

# All-traffic mode and the CA

Default interception (Tier 1) sees the agent's LLM API traffic and nothing
else. Telemetry, update checks, package downloads, and any other host the
agent talks to go straight out, because only the base-URL variable was
redirected.

All-traffic mode (Tier 2) is the opt-in answer: omni becomes a real MITM
proxy for the child process, with its own certificate authority.

:::caution[Not yet implemented]
`--all-traffic` and the `ca` subcommand are accepted by the CLI, validated
against each agent's capabilities, and reported by `omni config show` — but
no CA is generated and no traffic beyond the LLM API is intercepted yet. A
session launched with `--all-traffic` today behaves as a Tier 1 session.
This page is the design and the constraints it must meet.
:::

## The two tiers

| | Tier 1 — default | Tier 2 — `--all-traffic` |
|---|---|---|
| How | one base-URL environment variable | HTTPS proxy + generated CA |
| Sees | the agent's LLM API calls | every HTTPS call the child makes |
| Requires | nothing | a CA the child's runtime trusts |
| Installs | nothing | nothing outside `~/.omni` |
| Failure mode | traffic simply is not intercepted | fails loudly, before launch |

Tier 1 is enough for what most people want omni for: recording what the
model was asked and what it answered, and eventually changing which model
answers. Tier 2 exists for the questions Tier 1 cannot answer — what else is
this agent talking to, and what is it sending?

## Trust is scoped, never installed

The CA lives in `~/.omni/ca/`, created with `0700` permissions, and is
generated lazily on first use rather than at `omni init`.

It is never added to your system trust store. Trust is granted per runtime,
per process, through the environment of the child omni launches:

```sh
NODE_EXTRA_CA_CERTS=/Users/me/.omni/ca/ca.pem      # Node — Claude Code
```

The difference matters. A CA in your system store is trusted by your browser,
your package manager, and every process on the machine, for as long as it is
installed, and it outlives the tool that put it there. A CA in one
environment variable is trusted by one child process, for the length of one
session, and disappears with it.

That is why omni will never offer to install a certificate for you, and why
it does not ask for root.

## Agents that cannot support it

Per-runtime trust is exactly as available as the runtime makes it:

- **Node** reads `NODE_EXTRA_CA_CERTS`. Reliable — this is how Claude Code is
  supported.
- **Go** binaries generally respect `SSL_CERT_FILE`.
- **Rust with rustls and bundled roots** ignores every CA environment
  variable there is. For such an agent, Tier 2 is not merely unconfigured —
  it is impossible without patching the binary.

omni's answer is to refuse rather than to degrade. An agent with no confirmed
trust mechanism has none recorded in its profile, and `--all-traffic` on it
is a usage error:

```
omni: agent "codex" does not support --all-traffic
  no confirmed CA trust mechanism for this agent's runtime.
  its LLM traffic is still intercepted without --all-traffic.
```

The alternative — accepting the flag and quietly intercepting nothing extra —
would leave you believing you were seeing all traffic while you were not.
For a feature whose entire purpose is finding out what an agent does behind
your back, that is the one failure mode that cannot be allowed.

See [Claude Code](../../agents/claude-code/) and [Codex](../../agents/codex/)
for each agent's current status.
