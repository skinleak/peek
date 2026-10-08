#!/bin/sh
# Install peek from GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/skinleak/peek/main/install.sh | sh
#
# Environment variables:
#   PEEK_VERSION       version to install, e.g. 0.1.0 (default: latest)
#   PEEK_INSTALL_DIR   where to put the binary (default: /usr/local/bin if
#                      writable, otherwise ~/.local/bin)
set -eu

repo="skinleak/peek"

say() { printf '%s\n' "$*"; }
fail() { printf 'peek install: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "unsupported OS $(uname -s); peek supports Linux and macOS" ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) fail "unsupported architecture $(uname -m)" ;;
esac

version="${PEEK_VERSION:-}"
if [ -z "$version" ]; then
  # The releases/latest page redirects to .../tag/vX.Y.Z; this avoids API rate limits.
  latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
    fail "could not determine the latest release"
  version="${latest##*/}"
fi
version="${version#v}"
case "$version" in
  "" | latest | releases) fail "no release found for $repo" ;;
esac

archive="peek_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/v$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Downloading peek $version for $os/$arch..."
curl -fsSL -o "$tmp/$archive" "$base/$archive" || fail "download failed: $base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "could not download checksums"

expected=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d ' ' -f 1)
[ -n "$expected" ] || fail "no checksum listed for $archive"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$archive" | cut -d ' ' -f 1)
else
  actual=$(shasum -a 256 "$tmp/$archive" | cut -d ' ' -f 1)
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" peek

dir="${PEEK_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ]; then
    dir=/usr/local/bin
  else
    dir="$HOME/.local/bin"
  fi
fi
mkdir -p "$dir"
mv "$tmp/peek" "$dir/peek"
chmod 755 "$dir/peek"

say "Installed peek $version to $dir/peek"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "Note: $dir is not on your PATH. Add it with:  export PATH=\"$dir:\$PATH\"" ;;
esac
