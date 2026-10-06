#!/usr/bin/env bash
set -euo pipefail

# Cooperative distributed scanning with shared work queue
# All nodes coordinate via a shared queue file (NFS/rsync/shared FS)
QUEUE="/tmp/truffles-workqueue.json"
REPOS="repos.txt"

# Start master/first node or just init queue
./truffles coop -f "$REPOS" -queue "$QUEUE" -job master -format csv &
MASTER=$!
# Start slave nodes
./truffles coop -f "$REPOS" -queue "$QUEUE" -job slave1 -format csv &
./truffles coop -f "$REPOS" -queue "$QUEUE" -job slave2 -format csv &
wait $MASTER
wait
