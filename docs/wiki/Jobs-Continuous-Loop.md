# Jobs and the 24/7 loop

Use **`truffles job`** for continuous **search → cluster scan → clean-repos update**.

On macOS, `job start` wraps the loop with **`caffeinate -dims`** so closing the laptop lid does not pause search (workers on GCE keep scanning either way).

## What a job does

Each cycle (in-process `job run`):

1. **Search** — `truffles search` with `--prefix-file`, `-skip-file`, `-clean-file`, `-limit`, `-seed`
2. **Cluster run** — `truffles cluster run` with the same playbook (remote workers scan)
3. **Collect** — combined `results.csv` and **`results-verified.csv`** under `data/remote-results-<timestamp>/`
4. **Update** — `truffles clean rebuild --include-scanned` refreshes `data/clean-repos.txt`
5. **Sleep** — default 60s, then repeat

Job metadata lives in `data/jobs/<id>/meta.json`; logs in `data/jobs/<id>/job.log`.

## Start (correct way)

From the repo root, with `remote.env` available (the loop loads it):

```bash
cd /path/to/truffles
go build -o ./truffles ./cmd/truffles

./truffles job start -c remote-playbook.yaml \
  --prefix-file examples/prefixes-openai.txt \
  --limit 500 \
  --sleep 60
```

The command prints a **real job id** (e.g. `j-20261011-185522`). Save it.

Optional flags:

| Flag | Meaning |
|------|---------|
| `--once` | Single cycle then exit |
| `--full-playbook` | Run `truffles playbook` each cycle instead of search+cluster |
| `--terms-file` | Search terms file |
| `--seed` | Shuffle seed (`0` = random each cycle) |
| `--skip-file` / `--clean-file` | Override skip/clean paths |
| `--workers` | Search concurrency (default 8) |
| `--data-dir` | Data directory (default `data`) |
| `--bin` | Path to truffles binary (default: this executable) |

## Monitor (do not mix with stop)

```bash
./truffles job list
./truffles job status j-20261011-185522
./truffles job logs j-20261011-185522 -f    # follow; Ctrl+C only exits the log viewer
```

**Never** paste placeholder ids like `j-YYYYMMDD-HHMMSS` — use the id from `job start` or `job list`.

## Stop and restart (demo)

Use this when you need a **new binary**, different `--limit` / prefix file, or a clean break mid-cycle.

### Step 1 — Note the running job id

```bash
./truffles job list
# example: j-20261011-185522  running  10911
```

### Step 2 — Stop the local job process group

```bash
./truffles job stop j-20261011-185522
```

This sends SIGTERM (then SIGKILL) to the loop’s process group. It does **not** always stop remote trufflehog on workers immediately.

### Step 3 — Stop remote scans on all workers

Always run this after stopping a job if a cluster scan might still be running:

```bash
./truffles cluster stop -c remote-playbook.yaml
```

### Step 4 — (Optional) Pull partial results from the current cycle

If you stopped during cluster run, collect whatever workers finished:

```bash
# Replace with the timestamped dir from job.log ("cluster run … → data/remote-results-…")
./truffles cluster collect -c remote-playbook.yaml \
  -out remote-results-20261011-185522 \
  -data-dir data -y
```

Open the verified spreadsheet:

```bash
open data/remote-results-20261011-185522/results-verified.csv
```

### Step 5 — Rebuild and start a **new** job

Each start creates a **new** id; you do not “restart” the old id.

```bash
go build -o ./truffles ./cmd/truffles

./truffles job start -c remote-playbook.yaml \
  --prefix-file examples/prefixes-openai.txt \
  --limit 500 \
  --sleep 60
```

### Step 6 — Confirm

```bash
./truffles job list
./truffles job status j-<NEW-ID>
./truffles job logs j-<NEW-ID> -f
```

## One-shot copy-paste (kill, start fresh)

Replace the id if yours differs:

```bash
cd /path/to/truffles

./truffles job stop j-20261011-185522
./truffles cluster stop -c remote-playbook.yaml

go build -o ./truffles ./cmd/truffles

./truffles job start -c remote-playbook.yaml \
  --prefix-file examples/prefixes-openai.txt \
  --limit 500 \
  --sleep 60

./truffles job list
```

## Only one loop job

Running **two** `job start` processes searches the same prefixes twice and doubles load on GitHub and workers. Before starting, check:

```bash
./truffles job list
# stop any status=running job first
```

## Foreground loop (debug only)

```bash
./truffles job run -c remote-playbook.yaml \
  --prefix-file examples/prefixes-openai.txt --limit 500 --sleep 60 --once
```

Prefer `truffles job start` for logging, ids, and caffeinate.

## Related

- [Cluster and GCE workers](Cluster-and-GCE-Workers) — deploy, heal, collect
- [Verify and results](Verify-and-Results) — consolidate / combine
- [Skip lists and clean repos](Skip-Lists-and-Clean-Repos)
