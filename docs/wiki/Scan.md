# Scan

`truffles scan` runs trufflehog against a repo list (full git history cloned locally unless constrained).

## Basic usage

```bash
./truffles scan -f repos.txt
```

## Important flags

| Flag | Notes |
|------|-------|
| `-w` / `-workers` | Parallel repos (default 4) |
| `-results` | Default `verified,unknown` — tune for noise |
| `-format` | `csv` (default), `pretty`, `jsonl` |
| `-since` | Limit commits scanned after clone (default `2y`) |
| `-max-depth` | **Lossy** — last N commits only |
| `-no-verification` | Faster; all findings unverified |
| `-skip-file` / `-append-scanned` | Incremental runs |
| `-clean-file` | Track repos with zero verified secrets |
| `-exclude-paths` | Skip `node_modules`, fixtures, etc. |

## Status lines

- `[ok]` — clean (no findings in selected result types)
- `[++]` — findings
- `[!!]` — **not scanned** (error/timeout — never treated as clean)

Reports are fsynced per repo — safe to Ctrl+C (see [Interrupts and artifacts](Interrupts-and-Artifacts)).

## Remote scans

Local `scan` is for small lists. For scale use [Cluster and GCE workers](Cluster-and-GCE-Workers) or a [Playbook](Playbooks).
