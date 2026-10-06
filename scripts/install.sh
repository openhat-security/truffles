#!/usr/bin/env bash
set -euo pipefail

REPO="adamsiwiec/truffles"
NAME="truffles"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | cut -d'"' -f4 | head -n1)
if [ -z "${LATEST:-}" ]; then
  echo "Failed to fetch latest release"
  exit 1
fi

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
esac

URL="https://github.com/${REPO}/releases/download/${LATEST}/${NAME}_${LATEST#v}_${OS}_${ARCH}"
TMP=$(mktemp)
curl -fsSL -o "$TMP" "$URL"
chmod +x "$TMP"
sudo mv "$TMP" "$INSTALL_DIR/$NAME"
echo "Installed $NAME to $INSTALL_DIR/$NAME"
