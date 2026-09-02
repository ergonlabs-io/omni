---
title: Troubleshooting
summary: What omni's errors mean, how to tell an omni failure from an agent
  failure, and the first things to check when something is not intercepted.
last_updated: 2026-08-31
related:
  - ../reference/cli/
  - ../configuration/configuration-file/
  - ../interception/how-it-works/
---

# Troubleshooting

## Is this omni or the agent?

Every message omni originates is on stderr and starts with `omni: `. The
agent's output never does.

```
omni: agent "cluade" not found
```

The same rule applies over HTTP: a response omni generated carries
`X-Omni-Error`, and an error forwarded from the provider does not. And it
applies to exit codes — when the agent ran, omni exits with the agent's exit
code; when omni failed instead, it uses a `sysexits.h` code (`64`, `66`,
`69`, `70`, `78`). See [exit codes](../cli/#exit-codes).

## Common errors

### `agent "..." not found`

```
omni: agent "cluade" not found

  did you mean: claude

  known agents: claude, codex
  add your own: ~/.omni/profiles.d/<name>.conf
```

Exit `64`. The name is not a known agent or alias. Note that
`profiles.d/` is not read yet — see
[Custom profiles](../../agents/custom-profiles/).

### `binary "..." not found on PATH`

```
omni: agent "claude": binary "claude" not found on PATH
  install claude, or set `binary` under [agents.claude] in ~/.omni/omni.conf
```

Exit `69`. omni knows the agent but cannot find its executable. Install it,
or pin the path:

```toml
# ~/.omni/omni.conf
[agents.claude]
binary = "/opt/claude/bin/claude"
```

### `unknown flag`

```
omni: unknown flag --resume

  omni's own flags must come before the agent name.
  everything after the agent name is passed to it verbatim:
    omni --mode route claude --some-claude-flag
```

Exit `64`. An agent flag was placed before the agent name. Move it after:
`omni claude --resume`.

### `refusing to load` / `refusing to bind`

Exit `78`. One configuration problem is fatal rather than advisory:

- A value somewhere in config looks like an API key or bearer token.
  Credentials belong in the environment or the agent's own auth, never in a
  config file.

`refusing to bind` comes from the proxy, not from config: it holds live
credentials and will not bind anywhere reachable off-host. There is no
setting involved.

`omni config check` reports both without launching anything.

### `cannot apply --model-map`

Exit `64`. Model rewriting requires a wire format omni can decode, which
today means the Anthropic Messages API. See
[Model routing](../../interception/model-routing/).

### `does not support --all-traffic`

Exit `64`. The agent's runtime has no confirmed way to trust omni's CA, so
full MITM would silently intercept nothing. Its LLM traffic is still
intercepted without the flag. See
[All-traffic mode](../../interception/all-traffic/).

### `not yet implemented`

Exit `70`. `ca`, `sessions`, and `completions` are accepted names with no
implementation behind them yet.

## The agent retries in a loop and will not say why

```
401 User not found. · Retrying in 1s · attempt 4/10
```

This is the agent's own output, not omni's — no `omni: ` prefix. The agent is
reporting that *something* rejected its request, but it does not know a
routing rule sent that request somewhere other than its provider, so it cannot
tell you which backend said no.

Re-run with `-v` and omni will:

```console
$ omni -v claude
omni: route: backend "openrouter" returned 401 for minimax/minimax-m3:free
  (routed from claude-haiku-4-5): {"error":{"message":"User not found.","code":401}}
```

A `401` or `403` here almost always means the backend's key is dead or
revoked rather than missing — a key that is missing entirely stops the launch
before the agent ever starts. Check it against the provider directly, and if
it is stale, replace it in `~/.omni/credentials` or in your environment. See
[when a routed request fails](../../interception/model-routing/#when-a-routed-request-fails)
for the other statuses.

If no `omni: route:` line appears at all, no rule matched — the request went
to the agent's normal provider and the failure is between the agent and its
own account.

## Nothing is being recorded

Check, in order:

```sh
omni --dry-run claude          # mode, redact, and their sources
ls -la ~/.omni/sessions        # does the directory exist and is it writable?
omni -v claude                 # prints the proxy address and session directory
```

- `mode = "off"` from any layer disables it. `--dry-run` names the file and
  line that set it.
- Recording is fail-open by design: a recorder that cannot be created prints
  a warning to stderr and the session continues without it. Run with `-v` or
  read stderr rather than assuming silence means success.
- If the agent is not sending to the base URL omni set — a wrong or
  unconfirmed steering variable — the proxy sees nothing at all. That is the
  known open question for [Codex](../../agents/codex/).

## The agent is talking to something omni does not see

Expected. Default interception redirects one base URL: the LLM API and
nothing else. Telemetry, update checks, and every other host go straight
out. Seeing them requires
[all-traffic mode](../../interception/all-traffic/), which is not
implemented yet.

## The TUI looks wrong, or resizing does not work

The agent runs in a real PTY, and `omni claude` should be
indistinguishable from `claude`. If it is not, that is a bug worth
reporting, with one exception: when stdin is not a terminal — piped input,
CI — omni skips raw mode and resize tracking deliberately, so the session
runs but does not resize.

## A session hangs

omni sets no request or response timeout, on purpose: a long generation or a
long tool loop can legitimately hold a connection for minutes, and a proxy
that killed it would be worse than no proxy. So a hang is almost always the
agent or the provider, not the proxy.

`Ctrl-C` reaches the agent as it normally would. On shutdown, omni sends
`SIGTERM` to the child and escalates to `SIGKILL` after five seconds.

## Config is not doing what you expect

```sh
omni config show --agent claude
```

Every value is printed with the file and line, environment variable, or
layer that set it. With six layers, provenance is usually the whole answer
— most surprises are an `OMNI_*` variable inherited from a shell profile, or
a `.omni.conf` in the directory you happen to be in.
