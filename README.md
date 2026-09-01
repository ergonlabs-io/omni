<div align="center">

# omni

### One interception point for every coding agent you run.

**omni** launches coding-agent CLIs — [Claude Code](https://claude.com/claude-code),
[Codex](https://openai.com/codex/) — in a pseudo-terminal with their LLM traffic
redirected through a proxy on your own machine. That gives you a single place to
record, inspect, and eventually reroute every model call, across agents that
share no code with each other.

[![CI](https://github.com/ergonlabs-io/omni/actions/workflows/ci.yml/badge.svg)](https://github.com/ergonlabs-io/omni/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/ergonlabs-io/omni)](go.mod)
![Status: Phase 0](https://img.shields.io/badge/status-phase%200%20%C2%B7%20alpha-orange.svg)
![Platform: macOS and Linux](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey.svg)

</div>

```sh
omni claude          # launch Claude Code, record every LLM call
omni codex           # same, for Codex
omni --dry-run claude   # show what would happen; launch nothing
```

> [!WARNING]
> **Phase 0: recording and reconnaissance. Not ready for daily use.**
>
> The CLI, the configuration system, Tier 1 interception, and session recording
> are real and work today. Model routing, the capability adapter, and
> full-traffic MITM are designed but **not wired in** — a `model_map` you write
> today is validated and then ignored. See
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
                          │                │
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

- **🔒 Credentials redacted before they hit disk.** `Authorization`, `x-api-key`,
  and friends are stripped at capture time, on by default. Config files that
  contain a credential-shaped value are refused outright.

- **👻 Invisible by design.** omni's diagnostics go to stderr; stdout belongs
  to the child, because any byte omni writes there corrupts a full-screen TUI.
  Arguments after the agent name pass through untouched, and the child's exit
  code becomes omni's. `make smoke` guards it in the build.

- **🚦 Model routing, on the wire.** *(designed, not yet implemented)* Rewrite
  `claude-opus-5` to `claude-sonnet-5` for an agent that will not let you.

## What works today

This project documents what it does, not what it intends to do.

| | Capability | Status |
|---|---|---|
| ✅ | Session recording to `~/.omni/sessions` | Works |
| ✅ | Tier 1 interception (base-URL redirect to loopback) | Works |
| ✅ | Header redaction | Works, on by default |
| ✅ | Layered config, 7 precedence layers with provenance | Works |
| ✅ | PTY, raw mode, `SIGWINCH`, signal forwarding, exit codes | Works |
| ✅ | `omni init`, `config show`, `config check`, `config path` | Works |
| ✅ | `--dry-run`, `--version`, `--mode`, `--verbose` | Works |
| 🚧 | `model_map` / `mode = "route"` | Parsed and validated; the rewrite is not implemented |
| 🚧 | Capability adapter | Designed |
| 🚧 | `--all-traffic` (Tier 2 full MITM) | Validated per agent; no CA is generated yet |
| 🚧 | `omni sessions`, `omni ca`, `omni completions` | Reserved; exit with *not yet implemented* |
| 🚧 | `record.bodies`, `record.retention`, `adapt.*` | Accepted, not yet applied |

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

Claude Code, exactly as it always behaves, with every LLM call recorded to
`~/.omni/sessions/`.

> [!TIP]
> Everything after the agent name belongs to the agent, verbatim. That means
> `omni claude --help` prints *Claude Code's* help — use `omni --help` for
> omni's. `omni run <agent>` is the always-unambiguous form.

## Recorded sessions

One directory per session, named for when it started and which agent ran:

```
~/.omni/sessions/2026-08-31T09-14-02-claude/
├── meta.json
├── 001.request.headers.json
├── 001.request.json
├── 001.response.headers.json
├── 001.response.sse
└── ...
```

Bodies are stored verbatim rather than reformatted — a prettified copy answers
a different question than the one you will be asking. The response extension is
chosen from `Content-Type`, so streams and non-streams are distinguishable
without opening them. `meta.json` carries the agent, omni version, timings,
exchange count, and a token summary.

> [!NOTE]
> Recorded sessions can contain source code and credentials from your working
> directory. Redaction covers credential *headers*; it does not sanitize
> request bodies. Treat `~/.omni/sessions` as sensitive.

## Configuration

TOML, at `~/.omni/omni.conf`, with per-agent overrides in
`~/.omni/agents/<agent>.conf` and a per-project `.omni.conf`. Seven precedence
layers, and `omni config show` reports which one won for every value.

```toml
# ~/.omni/omni.conf
[defaults]
mode = "record"        # off | record | route

[defaults.record]
redact = true          # strip Authorization / x-api-key / *-api-key

[defaults.proxy]
listen = "127.0.0.1:0" # loopback only; a non-loopback bind is refused
```

```sh
omni config show --agent claude   # effective values + provenance
omni config check                 # validate; non-zero on error
omni config path                  # where omni thinks its home is
```

Two things are fatal rather than advisory: a non-loopback `proxy.listen`, and a
credential-shaped value anywhere in a config file.

## Roadmap

| Phase | | |
|---|---|---|
| **0** | Recorder and reconnaissance | **In progress** |
| 1 | Wire schema, byte-identical round-trip | Next |
| 2 | Model rewriting and the capability adapter | Designed |
| 3 | Middleware chain as an extension point | Designed |
| 4 | Tier 2 full MITM, opt-in | Designed |
| 5 | Second provider | Later |

Recording comes first because every later phase is written against the corpus
it produces. Cross-provider translation — making Codex talk to Anthropic — is
an explicit non-goal.

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
