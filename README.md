# omni

Run any coding agent CLI through a controlled interception proxy.

```sh
omni claude          # launch Claude Code; record all LLM traffic
omni codex           # same, for Codex
omni --mode route --model-map claude-opus-5=claude-sonnet-5 claude
```

`omni` launches the agent in a PTY with its API traffic redirected through a
local proxy. That gives one interception point — recording, model routing,
policy — that works the same across agents which share no code.

## Why a proxy

Agents pin their model names and validate them client-side, so config overrides
cannot reach the cases we care about. A proxy can: the agent emits a name it
approves of, and the substitution happens on the wire.

## Status

**Phase 0 — in progress.** Recording and reconnaissance. Not yet usable.

The CLI, the configuration system, and the installer are real; interception
behavior is still landing. The phased plan lives in `internal-docs/`, which is
not published.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/ergonlabs-io/omni/main/install.sh | sh
```

Downloads the latest release for your platform, verifies it against the
release `checksums.txt`, installs to `~/.local/bin`, and runs `omni init` to
create the `~/.omni` tree.

```sh
sh install.sh --dir /usr/local/bin      # somewhere else (OMNI_INSTALL_DIR)
sh install.sh --version v0.3.0          # pin a release (OMNI_VERSION)
sh install.sh --no-init                 # binary only; run `omni init` later
```

macOS and Linux, amd64 and arm64. `OMNI_HOME` relocates the config tree;
`GITHUB_TOKEN` authenticates the release lookup.

## Build

```sh
make check     # gofmt + vet + test
make build     # -> bin/omni
make smoke     # verify CLI invariants
make dist      # cross-compiled release tarballs + checksums -> dist/
```

`make dist` produces exactly the artifact names `install.sh` downloads
(`omni_<version>_<os>_<arch>.tar.gz` plus `checksums.txt`); the two change
together.

## Docs

User documentation is an Astro + Starlight site in [docs/site/](docs/site/),
published at `ergonlabs.io/docs/omni/`.

```sh
make docs-dev      # dev server at localhost:4321
make docs-build    # production build to docs/site/dist/
```

Design docs live in `internal-docs/` and are deliberately untracked — they are
the reasoning behind the code, not documentation for people who clone it. The
two that carry the most weight are 04 (model rewriting is a capability
adapter, not a string substitution) and 05 §1 (prompt-cache economics — the
most expensive way this project can fail).

## License

TBD
