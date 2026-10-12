#!/usr/bin/env bash
# Thin wrapper — the loop lives in the binary now.
# Prefer:  ./truffles job start -c remote-playbook.yaml
# Debug:   ./truffles job run -c remote-playbook.yaml --once
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
BIN="${TRUFFLES_BIN:-./truffles}"
if [[ ! -x "$BIN" && ! -f "$BIN" ]]; then
  echo "[!] missing $BIN — run: go build -o ./truffles ./cmd/truffles" >&2
  exit 1
fi
exec "$BIN" job run "$@"
