<div align="center">

# omni

### One interception point for every coding agent you run.

**omni** launches coding-agent CLIs — [Claude Code](https://claude.com/claude-code),
[Codex](https://openai.com/codex/) — in a pseudo-terminal with their LLM traffic
redirected through a proxy on your own machine. That gives you a single place to
record, inspect, and reroute every model call, across agents that share no
code with each other.

[![CI](https://github.com/ergonlabs-io/omni/actions/workflows/ci.yml/badge.svg)](https://github.com/ergonlabs-io/omni/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/ergonlabs-io/omni)](go.mod)
![Status: alpha](https://img.shields.io/badge/status-phase%200%2B2%20%C2%B7%20alpha-orange.svg)
![Platform: macOS and Linux](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey.svg)

</div>

```sh
omni claude             # launch Claude Code through the proxy
omni --record claude    # same, and keep a session recording
omni --dry-run claude   # show what would happen; launch nothing
```

> [!WARNING]
> **Phase 0/2: recording, plus model routing. Not yet ready for daily use.**
>
> The CLI, the configuration system, Tier 1 interception, session recording,
> and model routing are real and work today. The capability adapter and
> full-traffic MITM are designed but **not wired in**. See
> [What works today](#what-works-today) for the line-by-line status.

---

## Why a proxy

Agents pin their model names and validate them client-side, so configuration
overrides cannot reach the cases worth changing. A proxy can: the agent emits a
model name it approves of, and the substitution happens on the wire.

The same position that lets you rewrite a request lets you *see* it — which is
what omni does first, and the reason recording ships before routing.

```
your terminal ──PTY──▶ claude ──HTTP──▶ omni ──HTTPS──▶ api.anthropic.com
                          │                │              or a routed backend
             byte passthrough ─┘           └─ recorded, then forwarded
```

Two data paths that never meet. The terminal path is a pure byte pipe carrying
ANSI escapes from a redrawing TUI; omni never interprets a byte of it. The
network path is structured, and is the only place omni does anything at all.

## Why omni

- **🔍 See exactly what your agent sends.** Every request, response, and SSE
  stream written to disk byte for byte — not prettified, not summarized. What
  is on disk is what crossed the wire.

- **🪶 Nothing installed. No CA, no root, no system changes.** Tier 1
  interception is one environment variable, scoped to one child process, gone
  when the agent exits.

- **🧩 One control plane across agents that share no code.** Claude Code and
  Codex have nothing in common internally. Through omni they have one config
  system, one session format, and one place to intervene.

- **🔒 Credentials never reach disk, or a third party.** `Authorization`,
  `x-api-key`, and anything `*-api-key` have their values replaced with
  `[REDACTED]` at capture time, on by default — the header name is kept, so a
  session still shows which auth shape was used. The same predicate decides
  what is removed before a request is routed to another backend. Config files
  containing a credential-shaped value are refused outright.

- **👻 Invisible by design.** omni's diagnostics go to stderr; stdout belongs
  to the child, because any byte omni writes there corrupts a full-screen TUI.
  Arguments after the agent name pass through untouched, and the child's exit
  code becomes omni's. `make smoke` guards it in the build.

- **🚦 Model routing, on the wire.** Rewrite `claude-opus-5` to
  `claude-sonnet-5` for an agent that will not let you — or send everything
  matching a glob to a different provider, leaving the rest on Anthropic.

## What works today

This project documents what it does, not what it intends to do.

| | Capability | Status |
|---|---|---|
| ✅ | Session recording to `~/.omni/sessions` | Works, opt-in (`--record`) |
| ✅ | Tier 1 interception (base-URL redirect to loopback) | Works |
| ✅ | Header redaction | Works, on by default |
| ✅ | Layered config, 7 precedence layers with provenance | Works |
| ✅ | PTY, raw mode, `SIGWINCH`, signal forwarding, exit codes | Works |
| ✅ | `omni init`, `config show`, `config check`, `config path` | Works |
| ✅ | `--dry-run`, `--version`, `--mode`, `--record`, `--verbose`, `--model-map` | Works |
| ✅ | `[[route]]` rules and `[backends.*]` | Works |
| ✅ | `[backends.*]` routing to another provider | Works |
| 🚧 | Capability adapter | Designed |
| 🚧 | `--all-traffic` (Tier 2 full MITM) | Validated per agent; no CA is generated yet |
| 🚧 | `omni sessions`, `omni ca`, `omni completions` | Reserved; exit with *not yet implemented* |

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/ergonlabs-io/omni/main/install.sh | sh
```

Downloads the release for your platform, verifies it against the release
`checksums.txt`, installs to `~/.local/bin`, and runs `omni init` to create the
`~/.omni` tree. macOS and Linux, amd64 and arm64.

<details>
<summary>Installer options</summary>

```sh
sh install.sh --dir /usr/local/bin      # somewhere else (OMNI_INSTALL_DIR)
sh install.sh --version v0.1.0          # pin a release (OMNI_VERSION)
sh install.sh --no-init                 # binary only; run `omni init` later
```

| Flag | Environment | Meaning |
|---|---|---|
| `--version <tag>` | `OMNI_VERSION` | Release to install. Default: latest. |
| `--dir <path>` | `OMNI_INSTALL_DIR` | Install location. Default `~/.local/bin`. |
| `--no-init` | `OMNI_NO_INIT=1` | Skip creating `~/.omni`. |
| | `OMNI_HOME` | Relocate the config tree. |
| | `GITHUB_TOKEN` | Authenticate the release lookup. |

The installer is POSIX `sh` on purpose: it runs before anything is installed.
It verifies a SHA-256 checksum before installing and refuses to continue if the
release publishes none.

</details>

<details>
<summary>Build from source</summary>

Needs Go 1.26+. The result is a single static binary with no runtime
dependencies.

```sh
git clone https://github.com/ergonlabs-io/omni.git
cd omni
make build          # -> bin/omni
make install        # -> $PREFIX/bin/omni (default /usr/local)
```

</details>

## Quick start

**1. Set up `~/.omni`.**

```sh
omni init
```

Creates the config tree. It is idempotent and never rewrites a file that
already exists, so anything you edit stays edited.

**2. Look before you launch.**

```sh
omni --dry-run claude
```

Prints the binary it resolved, the environment it would inject, and every
effective config value **with the file and line that set it**. Launches
nothing, binds nothing.

**3. Run your agent.**

```sh
omni claude
```

Claude Code, exactly as it always behaves, with every LLM call passing through
omni. Add `--record` to keep the traffic:

```sh
omni --record claude
```

Recording is off unless you ask for it, because a recording is your prompts —
and the source and secrets they carry — written to disk.

> [!TIP]
> Everything after the agent name belongs to the agent, verbatim. That means
> `omni claude --help` prints *Claude Code's* help — use `omni --help` for
> omni's. `omni run <agent>` is the always-unambiguous form.

## Recorded sessions

Recording is opt-in: `omni --record <agent>`, or `record.enabled = true` in
config. When it is on, you get one directory per session, named for when it
started and which agent ran:

```
~/.omni/sessions/2026-08-31T09-14-02-claude/
├── meta.json
├── exchanges.jsonl
├── 001.request.json
├── 001.response.sse
└── ...
```

`exchanges.jsonl` is the index: one JSON line per request and per response, in
the order they happened, carrying method, URL, status, headers, time-to-first-byte,
and the name of the body file. Bodies live in their own files and are stored
verbatim rather than reformatted or escaped — a prettified copy answers a
different question than the one you will be asking, and a byte-identical copy is
what makes a session replayable. The response extension is chosen from
`Content-Type`, so streams and non-streams are distinguishable without opening
them. `meta.json` carries the agent, omni version, timings, exchange count, and a
token summary.

```sh
jq -r 'select(.type=="response") | "\(.seq) \(.status) \(.ttfb_ms)ms"' exchanges.jsonl
```

> [!NOTE]
> Recorded sessions can contain source code and credentials from your working
> directory. Redaction covers credential *headers*; it does not sanitize
> request bodies. Treat `~/.omni/sessions` as sensitive.

## Configuration

TOML, at `~/.omni/omni.conf` — one file holding global defaults, backends,
and per-agent settings. A per-project `.omni.conf` adds a restricted
repo-local layer. Six precedence layers, and `omni config show` reports which
one won for every value.

```toml
# ~/.omni/omni.conf
[defaults]
mode   = "record"       # off | record — intercept and route
record.enabled = false  # write sessions to disk; off by default
redact = true           # strip Authorization / x-api-key / *-api-key
```

That is the whole of `[defaults]`. Anything omni cannot actually do yet has
no key: a setting exists here only if something reads it.

```sh
omni config show --agent claude   # effective values + provenance
omni config check                 # validate; non-zero on error
omni config path                  # where omni thinks its home is
```

### Routing a model somewhere else

Writing a rule turns routing on; there is no mode to remember. Rules are an
ordered list, first match wins; `match` is a glob against the model the agent
asked for. A rule sends
the request to a `backend`, renames the model in place with `model`, or both.
Anything unmatched goes to the agent's own upstream, untouched.

```toml
# ~/.omni/omni.conf — one file; backends are global, rules are per-agent
[backends.openrouter]
base_url    = "https://openrouter.ai/api"   # serves the Messages API at /v1/messages
api_key_env = "OPENROUTER_API_KEY"          # the name; never the key itself
api_style   = "anthropic"
model       = "minimax/minimax-m3:free"

[[agents.claude.route]]
match   = "claude-haiku-4-5*"               # alias and dated id, one rule
backend = "openrouter"

[[agents.claude.route]]
match = "claude-opus-*"
model = "claude-sonnet-5"                   # same provider, cheaper model
```

Rules are ordered because globs overlap, and a TOML table has no defined
iteration order — a map of patterns would resolve differently from run to
run. `omni config check` rejects a rule naming an unknown backend and warns
about one an earlier rule already covers.

omni routes; it does not translate. A backend must speak the agent's own wire
format, so `api_style` is checked against the agent's and a mismatch is
rejected at load rather than forwarded and hoped over — point `base_url` at a
translating gateway if you need one.

`api_key_env` names a variable, never a key: omni resolves it from its own
environment and then from `~/.omni/credentials`, a `0600` file of
`NAME=value` lines that is the one place in the tree a key may be written
down. A credential-shaped value anywhere in config is refused at load. The
choice between the two decides who else sees the key — a variable you export
is inherited by the agent omni launches; one kept in the credentials file is
used by omni alone.

Before a request leaves for a backend, the agent's credential headers are
replaced with the backend's own — `Authorization`, `x-api-key`, and anything
ending in `-api-key`, the same predicate the recorder redacts by. Those are
the only headers omni removes; `anthropic-beta`, `anthropic-version`, client
telemetry and everything else are forwarded verbatim, because omni does not
get to decide which headers a backend wants. That replacement is made **by
host, not by name**: credentials are forwarded only when the backend resolves
to the same host the agent would have reached anyway, so a backend called
`anthropic` that points somewhere else cannot talk omni into leaking a token
there.

Two things a project-local `./.omni.conf` deliberately cannot do: declare a
backend, or write a rule naming one. Renaming a model is a local preference;
choosing who receives your prompts and bills you for them is not a decision a
repository you cloned gets to make.

One thing is fatal rather than advisory: a credential-shaped value anywhere
in a config file. The proxy's loopback-only bind is not a config key at all —
it is simply what omni does.

## Roadmap

| Phase | | |
|---|---|---|
| **0** | Recorder and reconnaissance | **In progress** |
| 1 | Wire schema, byte-identical round-trip | **Skipped so far** — see below |
| 2 | Model rewriting and backend routing | **Done**; capability adapter designed |
| 3 | Middleware chain as an extension point | Partly — routing uses the seam; not yet a public one |
| 4 | Tier 2 full MITM, opt-in | Designed |
| 5 | Second provider | Later |

Recording comes first because every later phase is written against the corpus
it produces. Cross-provider translation — making Codex talk to Anthropic — is
an explicit non-goal.

> [!NOTE]
> **Phase 2 landed before Phase 1, which the plan advised against.** Phase 1
> is the byte-identical round-trip harness — the gate that proves omni can
> take a request apart and put it back together without perturbing a cache
> breakpoint, and so without silently costing you money.
>
> What stands in for it today: a request no rule matches is never decoded or
> re-encoded at all, and rewriting a matched one splices the single `model`
> string value in place rather than re-serializing the document, so key
> order, whitespace, escapes, and every `cache_control` marker survive byte
> for byte. Both are pinned by tests. What is still missing is the
> round-trip corpus replay that would prove it across real traffic rather
> than fixtures, and a cache-hit-rate comparison between a recorded baseline
> and a routed session. Until that exists, treat routing as working but
> unproven at scale.

## Docs

User documentation is an [Astro](https://astro.build) +
[Starlight](https://starlight.astro.build) site in [docs/site/](docs/site/).

```sh
make docs-dev      # dev server at localhost:4321
make docs-build    # production build to docs/site/dist/
```

## Development

```sh
make check         # gofmt + vet + test  (the pre-commit gate)
make test-race     # race detector
make smoke         # verify the CLI invariants
make dist          # cross-compiled release tarballs + checksums
```

`make dist` produces exactly the artifact names `install.sh` downloads
(`omni_<version>_<os>_<arch>.tar.gz` plus `checksums.txt`); the two change
together, and CI fails a release if they ever drift.

Unix only. omni launches agents in a PTY, so Windows is out of scope.

## License

[MIT](LICENSE)
