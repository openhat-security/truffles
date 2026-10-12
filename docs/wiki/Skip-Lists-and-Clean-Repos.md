# Skip lists and clean repos

Incremental hunting avoids re-scanning the same repos and skips repos that already had **zero verified secrets**.

## Core log

Important updates (scanned/clean merges, search done, cluster collect, jobs) append to **`data/truffles.log`** with timestamps and absolute paths. Verbose proxy lines stay on stderr only (`-v`).

```bash
tail -f data/truffles.log
```

## Files (defaults in remote-playbook.yaml)

| File | Purpose |
|------|---------|
| `data/scanned-repos.txt` | One GitHub URL per line — already scanned successfully |
| `data/clean-repos.txt` | Repos scanned with **no verified** secrets — skip in search **and** scan |

Both are consulted during **search** (`-skip-file`, `-clean-file`) so `-limit` applies only to **new** candidates.

## During scan

- `-skip-file` — skip lines present in master list
- `-append-scanned` — append each finished repo URL (usually same file as skip)
- `-clean-file` — update when scan completes with zero verified rows (requires live verification)

## Rebuild clean list from CSV history

```bash
./truffles clean rebuild --include-scanned
```

The 24/7 loop runs this after each cycle.

## Why row count ≠ repo count

A CSV may have thousands of rows from few repos (many findings per repo) or many repos with one finding each. Use unique `repo` column or `scanned-*.txt` for repo counts.

## Clean JSON on workers

`clean-workerN.json` mirrors per-worker clean state during cluster runs; master text file is updated after collect + update script.
