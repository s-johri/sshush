#!/bin/sh
# sshush installer: download the latest release archive for this OS/arch, verify
# its checksum, and drop the `sshush` binary on your PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/s-johri/sshush/main/install.sh | sh
#
# Env overrides:
#   INSTALL_DIR   target directory (default: ~/.local/bin)
#   SSHUSH_VERSION  pin a release tag (default: latest), e.g. v0.7.0
set -eu

REPO="s-johri/sshush"
: "${INSTALL_DIR:=$HOME/.local/bin}"
: "${SSHUSH_VERSION:=latest}"

if [ "$SSHUSH_VERSION" = "latest" ]; then
	base="https://github.com/${REPO}/releases/latest/download"
else
	base="https://github.com/${REPO}/releases/download/${SSHUSH_VERSION}"
fi

# Map uname to the goreleaser asset naming (sshush_<os>_<arch>.tar.gz).
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
linux) os=linux ;;
darwin) os=darwin ;;
*) echo "sshush: unsupported OS '$os' (use Homebrew, the AUR, or a prebuilt archive)" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) echo "sshush: unsupported architecture '$arch'" >&2; exit 1 ;;
esac

asset="sshush_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "sshush: downloading ${asset} (${SSHUSH_VERSION})…"
curl -fsSL "${base}/${asset}" -o "$tmp/$asset"
curl -fsSL "${base}/checksums.txt" -o "$tmp/checksums.txt"

echo "sshush: verifying checksum…"
want=$(grep " ${asset}\$" "$tmp/checksums.txt" | awk '{print $1}')
if [ -z "$want" ]; then
	echo "sshush: no checksum for ${asset} in checksums.txt" >&2
	exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/$asset" | awk '{print $1}')
else
	got=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
fi
if [ "$want" != "$got" ]; then
	echo "sshush: checksum mismatch for ${asset}" >&2
	echo "  want $want" >&2
	echo "  got  $got" >&2
	exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp" sshush
mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/sshush" "$INSTALL_DIR/sshush"

echo "sshush: installed to ${INSTALL_DIR}/sshush"
case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*) echo "sshush: add ${INSTALL_DIR} to your PATH to run 'sshush'" >&2 ;;
esac
"$INSTALL_DIR/sshush" version 2>/dev/null || true
