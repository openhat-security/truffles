#!/usr/bin/env bash
# Search-only: find GitHub repos that mention known AI / LLM API key patterns.
# Each run shuffles query + page order (-seed 0) so you don't always walk the
# same first pages against scanned-repos.txt / clean-repos.txt.
#
# Usage (from repo root):
#   ./examples/search-ai-keys.sh
#   LIMIT=200 ./examples/search-ai-keys.sh
#   PREFIX_FILE=examples/prefixes.txt ./examples/search-ai-keys.sh   # all providers
#   PROXY_FILE=data/proxies.txt NO_DIRECT=1 ./examples/search-ai-keys.sh
#
# Requires: ./truffles built, GITHUB_TOKEN in env or remote.env
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [[ -f remote.env ]]; then
  set -a
  # shellcheck disable=SC1091
  source remote.env
  set +a
fi

DATA_DIR="${DATA_DIR:-data}"
LIMIT="${LIMIT:-500}"
PREFIX_FILE="${PREFIX_FILE:-examples/prefixes-openai.txt}"
OUT="${OUT:-$DATA_DIR/repos-ai-$(date +%Y%m%d-%H%M%S).txt}"
SKIP="${SKIP:-$DATA_DIR/scanned-repos.txt}"
CLEAN="${CLEAN:-$DATA_DIR/clean-repos.txt}"

if [[ ! -x ./truffles ]]; then
  echo "[!] build first: go build -o ./truffles ./cmd/truffles" >&2
  exit 1
fi

TOK=""
if [[ -n "${GITHUB_TOKEN:-}" ]]; then TOK="$GITHUB_TOKEN"; fi
if [[ -n "${GITHUB_TOKEN_2:-}" ]]; then
  [[ -n "$TOK" ]] && TOK="$TOK,$GITHUB_TOKEN_2" || TOK="$GITHUB_TOKEN_2"
fi
if [[ -z "$TOK" ]]; then
  echo "[!] set GITHUB_TOKEN (code search needs a PAT)" >&2
  exit 1
fi

args=(
  search
  -prefix-file "$PREFIX_FILE"
  -out "$OUT"
  -skip-file "$SKIP"
  -clean-file "$CLEAN"
  -limit "$LIMIT"
  -seed 0
  -workers 8
  -token "$TOK"
  -retries 6
)
[[ -n "${PROXY_FILE:-}" ]] && args+=(-proxy-file "$PROXY_FILE")
[[ -n "${PROXY_FILE:-}" || "${NO_DIRECT:-}" == 1 ]] && args+=(-no-direct)

echo "[*] search-ai-keys → $OUT (limit=$LIMIT prefix=$PREFIX_FILE)"
exec ./truffles "${args[@]}"
