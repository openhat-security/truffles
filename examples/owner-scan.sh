#!/usr/bin/env bash
set -euo pipefail

# Example wrapper for owner enumeration + scan
# Usage: examples/owner-scan.sh BurntSushi '*llm*'

OWNER="${1:-BurntSushi}"
shift || true
PATTERNS=("$@")
OUT="repos-$(echo "$OWNER" | tr ',' '-')-$(date +%s).txt"

if [ ${#PATTERNS[@]} -eq 0 ]; then
  ./truffles search -owner "$OWNER" -out "$OUT"
else
  ./truffles search -owner "$OWNER" "${PATTERNS[@]}" -out "$OUT"
fi
./truffles scan -f "$OUT" -format pretty -workers 4
