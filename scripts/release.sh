#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

current="$(sed -n 's/.*Version = "\([^"]*\)".*/\1/p' internal/version/version.go | head -1)"
VERSION="${1:-$current}"
VERSION="${VERSION#v}"
TAG="v${VERSION}"
DATE="$(date +%Y-%m-%d)"

echo "Releasing $TAG from $current"
