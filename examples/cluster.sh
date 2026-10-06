#!/usr/bin/env bash
set -euo pipefail

# Distributed example via SSH
# Requires cluster.yaml with enabled slaves and SSH access
CLUSTER="${1:-examples/cluster.yaml}"
REPOS="${2:-repos.txt}"

./truffles cluster distribute -c "$CLUSTER" -f "$REPOS"
./truffles cluster run -c "$CLUSTER" -f "$REPOS" -w 4 -format csv
./truffles cluster collect -c "$CLUSTER" -out ./cluster-results
