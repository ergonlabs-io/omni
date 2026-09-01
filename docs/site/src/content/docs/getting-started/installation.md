---
title: Installation
summary: Install omni with the install script or from source, and understand
  what it creates under ~/.omni.
last_updated: 2026-08-31
related:
  - ../getting-started/introduction/
  - ../configuration/configuration-file/
  - ../reference/cli/
---

# Installation

omni is a single static binary. macOS and Linux, amd64 and arm64; there is no
Windows build, because the PTY launcher targets unix-like systems.

## Install script

```sh
curl -fsSL https://raw.githubusercontent.com/ergonlabs-io/omni/main/install.sh | sh
```

The script detects your platform, downloads the matching release archive,
verifies it against the release `checksums.txt`, installs the binary to
`~/.local/bin`, and runs `omni init` to create the `~/.omni` tree. A checksum
mismatch aborts the install; nothing is written.

It is idempotent — re-running it upgrades in place, and `omni init` never
overwrites a config file you have edited.

### Options

| Flag | Environment | Meaning |
|---|---|---|
| `--version <tag>` | `OMNI_VERSION` | Install a specific release instead of the latest. The leading `v` is optional. |
| `--dir <path>` | `OMNI_INSTALL_DIR` | Where the binary goes. Default `~/.local/bin`. |
| `--no-init` | `OMNI_NO_INIT=1` | Install the binary only; run `omni init` yourself later. |
| | `OMNI_HOME` | Root of the config tree. Default `~/.omni`; honored by `omni init`. |
| | `GITHUB_TOKEN` | Authenticates the release lookup — for private repositories and API rate limits. |
| | `OMNI_SKIP_CHECKSUM=1` | Install without verifying. Not recommended. |

```sh
sh install.sh --dir /usr/local/bin     # system-wide
sh install.sh --version v0.3.0         # pin a release
sh install.sh --no-init                # binary only
```

If the install directory is not on your `PATH`, the script says so and prints
the line to add for your shell.

## From source

Go 1.26 or newer.

```sh
git clone https://github.com/ergonlabs-io/omni
cd omni
make build           # -> bin/omni
make install         # -> $PREFIX/bin/omni, PREFIX defaults to /usr/local
```

`make dist` cross-compiles the release archives and writes the same
`checksums.txt` the install script verifies against — the artifact names in
`make dist` and the ones in `install.sh` are one contract and change
together.

## What gets created

`omni init` creates the configuration tree, and is safe to re-run:

```
~/.omni/
├── omni.conf              global config, fully commented
├── agents/
│   ├── claude.conf        per-agent drop-in
│   └── codex.conf
├── profiles.d/            your own agent profiles
├── ca/                    0700; the CA is generated lazily, not here
├── cache/
└── sessions/              recorded sessions
```

`~/.omni` and `ca/` are `0700`. Recorded sessions can contain source code and
credentials, so the home directory's permissions are set explicitly at
creation rather than left to your umask.

## Verify

```sh
omni --version
omni config path      # where omni thinks its home is
omni --help           # includes which agents were detected on PATH
```

`omni --help` reports each known agent as detected (with its resolved path)
or not found, which answers "is my agent installed" without a second command.

:::note[`omni init` writes opinionated defaults]
The generated `agents/claude.conf` is not empty. It ships `mode = "route"` and
a sample `model_map` mapping `claude-opus-5` to `claude-sonnet-5`. Neither is
applied in Phase 0, but both will be once routing lands. Run
`omni --dry-run claude` to see the effective config, and edit or delete
anything you did not ask for.
:::

Next: run your first session in the [Quickstart](../quickstart/).
