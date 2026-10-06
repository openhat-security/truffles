#!/usr/bin/env bash
set -euo pipefail

# Example wrapper: takes a search term and runs search + scan
# Usage: examples/search-scan.sh "llm"

TERM="${1:-llm}"
OUT="repos-$(date +%s).txt"
REPORT_FMT="pretty"

./truffles search "$TERM" -limit 100 -out "$OUT"
./truffles scan -f "$OUT" -format "$REPORT_FMT" -workers 4
