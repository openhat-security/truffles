#!/usr/bin/env bash
# Publish docs/wiki/*.md to GitHub Wiki (openhat-security/truffles.wiki).
#
# Usage:
#   ./scripts/sync-wiki.sh
#   WIKI_REPO=git@github.com:ORG/REPO.wiki.git ./scripts/sync-wiki.sh
#
# Requires: git, push access to the wiki repo (wiki must be enabled on GitHub).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/docs/wiki"
WIKI_SSH="${WIKI_SSH:-git@github.com:openhat-security/truffles.wiki.git}"
WIKI_HTTPS="${WIKI_HTTPS:-https://github.com/openhat-security/truffles.wiki.git}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

wiki_bootstrap() {
  cat >&2 <<'EOF'
[!] GitHub Wiki git remote is not available yet (common on first use).

Bootstrap once in the browser:
  1. Open https://github.com/openhat-security/truffles/wiki/_new
  2. Title: Home — save with any one-line placeholder
  3. Re-run: ./scripts/sync-wiki.sh

Wiki source in this repo: docs/wiki/
EOF
}

if [[ ! -d "$SRC" ]]; then
  echo "[!] missing $SRC" >&2
  exit 1
fi

shopt -s nullglob
pages=("$SRC"/*.md)
if [[ ${#pages[@]} -eq 0 ]]; then
  echo "[!] no markdown in $SRC" >&2
  exit 1
fi

if git clone --depth 1 "$WIKI_HTTPS" "$TMP/wiki" 2>/dev/null || git clone --depth 1 "$WIKI_SSH" "$TMP/wiki" 2>/dev/null; then
  :
else
  echo "[*] wiki clone failed — initializing fresh tree (needs wiki bootstrapped on GitHub)"
  mkdir -p "$TMP/wiki"
  git -C "$TMP/wiki" init -b master
  git -C "$TMP/wiki" remote add origin "$WIKI_HTTPS"
fi

for f in "${pages[@]}"; do
  base="$(basename "$f")"
  [[ "$base" == "README.md" ]] && continue
  cp "$f" "$TMP/wiki/$base"
done

cd "$TMP/wiki"
git add -A
if git diff --staged --quiet; then
  echo "[=] wiki already up to date"
  exit 0
fi

git -c user.name="${GIT_AUTHOR_NAME:-truffles wiki sync}" \
    -c user.email="${GIT_AUTHOR_EMAIL:-wiki@local}" \
    commit -m "Sync wiki from docs/wiki ($(date -u +%Y-%m-%dT%H:%M:%SZ))"

branch="$(git branch --show-current 2>/dev/null || echo master)"
echo "[+] pushing to $WIKI_HTTPS ($branch)"
if command -v gh >/dev/null 2>&1; then
  gh auth setup-git 2>/dev/null || true
fi
if ! git push -u origin "$branch" 2>&1; then
  wiki_bootstrap
  exit 1
fi

echo "[+] done — https://github.com/openhat-security/truffles/wiki"
