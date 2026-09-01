#!/bin/sh
# install.sh — install omni and set up its home directory.
#
#   curl -fsSL https://raw.githubusercontent.com/ergonlabs-io/omni/main/install.sh | sh
#
# Downloads the release binary for this platform, verifies its checksum,
# installs it, and runs `omni init` to create the ~/.omni tree.
#
# Options (flags or environment):
#   --version <tag>    OMNI_VERSION        release to install (default: latest)
#   --dir <path>       OMNI_INSTALL_DIR    where the binary goes (default: ~/.local/bin)
#   --no-init          OMNI_NO_INIT=1      skip creating ~/.omni
#                      OMNI_HOME           omni's root dir, honored by `omni init`
#                      GITHUB_TOKEN        auth for the release API (private repos, rate limits)
#                      OMNI_SKIP_CHECKSUM=1  install without verifying (not recommended)
#
# POSIX sh on purpose: this runs before anything is installed.

set -eu

REPO="${OMNI_REPO:-ergonlabs-io/omni}"
VERSION="${OMNI_VERSION:-latest}"
INSTALL_DIR="${OMNI_INSTALL_DIR:-$HOME/.local/bin}"
NO_INIT="${OMNI_NO_INIT:-}"
SKIP_CHECKSUM="${OMNI_SKIP_CHECKSUM:-}"
BIN=omni

info() { printf 'omni: %s\n' "$*" >&2; }
warn() { printf 'omni: warning: %s\n' "$*" >&2; }
die()  { printf 'omni: error: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# Prints the comment header above, stopping at the first line of code.
# Falls back to a one-liner when the script has no readable path (curl | sh).
usage() {
	if [ -r "$0" ]; then
		awk 'NR == 1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "$0"
	else
		echo "usage: install.sh [--version <tag>] [--dir <path>] [--no-init]"
	fi
	exit 0
}

while [ $# -gt 0 ]; do
	case "$1" in
		--version) [ $# -ge 2 ] || die "--version needs a value"; VERSION=$2; shift 2 ;;
		--version=*) VERSION=${1#*=}; shift ;;
		--dir) [ $# -ge 2 ] || die "--dir needs a value"; INSTALL_DIR=$2; shift 2 ;;
		--dir=*) INSTALL_DIR=${1#*=}; shift ;;
		--no-init) NO_INIT=1; shift ;;
		-h|--help) usage ;;
		*) die "unknown option $1 (try --help)" ;;
	esac
done

# ---------------------------------------------------------------- platform

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
	linux|darwin) ;;
	*) die "unsupported OS: $os
  omni launches agents in a PTY; only macOS and Linux are supported." ;;
esac

arch=$(uname -m)
case "$arch" in
	x86_64|amd64) arch=amd64 ;;
	arm64|aarch64) arch=arm64 ;;
	*) die "unsupported architecture: $arch" ;;
esac

# ---------------------------------------------------------------- fetching

if have curl; then
	dl_tool=curl
elif have wget; then
	dl_tool=wget
else
	die "need curl or wget to download the release"
fi

# http_get <url> <dest|->. GITHUB_TOKEN, when set, authenticates the API
# call; curl and wget both drop the header across the redirect to the asset
# CDN, which is what we want.
http_get() {
	_url=$1
	_out=$2
	if [ "$dl_tool" = curl ]; then
		if [ -n "${GITHUB_TOKEN:-}" ]; then
			curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" "$_url" -o "$_out"
		else
			curl -fsSL "$_url" -o "$_out"
		fi
	else
		if [ -n "${GITHUB_TOKEN:-}" ]; then
			wget -q --header="Authorization: Bearer $GITHUB_TOKEN" -O "$_out" "$_url"
		else
			wget -q -O "$_out" "$_url"
		fi
	fi
}

sha256_of() {
	if have sha256sum; then sha256sum "$1" | awk '{print $1}'
	elif have shasum; then shasum -a 256 "$1" | awk '{print $1}'
	elif have openssl; then openssl dgst -sha256 "$1" | awk '{print $NF}'
	else return 1
	fi
}

tmp=$(mktemp -d "${TMPDIR:-/tmp}/omni-install.XXXXXX") || die "cannot create temp dir"
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT HUP INT TERM

# ----------------------------------------------------------- resolve version

if [ "$VERSION" = latest ]; then
	http_get "https://api.github.com/repos/$REPO/releases/latest" "$tmp/latest.json" \
		|| die "cannot reach the GitHub release API for $REPO
  if the repository is private, set GITHUB_TOKEN
  or pin a release with --version <tag>"
	VERSION=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$tmp/latest.json" | head -1)
	[ -n "$VERSION" ] || die "no published release found for $REPO"
else
	case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac
fi

# Asset names carry the bare version; the tag carries the v. Both come from
# `make dist`, which is the only supported way to build a release.
bare=${VERSION#v}
asset="${BIN}_${bare}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$VERSION"

info "installing $BIN $VERSION ($os/$arch)"

http_get "$base/$asset" "$tmp/$asset" \
	|| die "no release asset $asset in $VERSION
  see https://github.com/$REPO/releases/tag/$VERSION for what was published"

# ------------------------------------------------------------- verify

if [ -n "$SKIP_CHECKSUM" ]; then
	warn "skipping checksum verification (OMNI_SKIP_CHECKSUM is set)"
elif http_get "$base/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
	expected=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt" | head -1)
	[ -n "$expected" ] || die "checksums.txt has no entry for $asset"
	actual=$(sha256_of "$tmp/$asset") \
		|| die "need sha256sum, shasum, or openssl to verify the download
  set OMNI_SKIP_CHECKSUM=1 to install without verifying"
	[ "$actual" = "$expected" ] || die "checksum mismatch for $asset
  expected $expected
  got      $actual
  do not use this download"
	info "checksum ok"
else
	die "release $VERSION publishes no checksums.txt
  set OMNI_SKIP_CHECKSUM=1 to install anyway"
fi

# ------------------------------------------------------------- install

tar -xzf "$tmp/$asset" -C "$tmp" || die "cannot extract $asset"
[ -f "$tmp/$BIN" ] || die "$asset does not contain a $BIN binary"

mkdir -p "$INSTALL_DIR" || die "cannot create $INSTALL_DIR"
[ -w "$INSTALL_DIR" ] || die "$INSTALL_DIR is not writable
  re-run with --dir ~/.local/bin, or with sudo for a system-wide install"

# Stage inside the target directory so the final rename is atomic and does
# not hit ETXTBSY when replacing a running omni.
staged="$INSTALL_DIR/.$BIN.install.$$"
cp "$tmp/$BIN" "$staged" || die "cannot write to $INSTALL_DIR"
chmod 0755 "$staged"
mv -f "$staged" "$INSTALL_DIR/$BIN" || { rm -f "$staged"; die "cannot install into $INSTALL_DIR"; }

info "installed $INSTALL_DIR/$BIN"

# ------------------------------------------------------------- set up ~/.omni

if [ -n "$NO_INIT" ]; then
	info "skipping home setup; run \`$BIN init\` when you want it"
else
	"$INSTALL_DIR/$BIN" init || die "\`$BIN init\` failed; the binary is installed but ~/.omni is not set up"
fi

"$INSTALL_DIR/$BIN" --version || die "the installed binary does not run on this machine"

# ------------------------------------------------------------- PATH

case ":${PATH:-}:" in
	*":$INSTALL_DIR:"*) ;;
	*)
		warn "$INSTALL_DIR is not on your PATH"
		case "$(basename "${SHELL:-sh}")" in
			fish) printf '  fish_add_path %s\n' "$INSTALL_DIR" >&2 ;;
			zsh)  printf '  echo '\''export PATH="%s:$PATH"'\'' >> ~/.zshrc\n' "$INSTALL_DIR" >&2 ;;
			*)    printf '  echo '\''export PATH="%s:$PATH"'\'' >> ~/.bashrc\n' "$INSTALL_DIR" >&2 ;;
		esac
		;;
esac

info "done — try \`$BIN --help\`"
