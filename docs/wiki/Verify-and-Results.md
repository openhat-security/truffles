# Verify and results

## Result files

After a **cluster collect**, each run directory contains:

| File | Contents |
|------|----------|
| `results-workerN.csv` | Per-worker trufflehog CSV |
| `results.csv` | Merged all workers |
| **`results-verified.csv`** | Rows where `verified=true` only — **use for triage/spreadsheet** |
| `results-workerN.txt` | Pretty text mirror |
| `scanned-workerN.txt` | Repos finished on that worker |
| `clean-workerN.json` | Per-worker clean metadata |

The merge + verified filter happens in `combineResultCSVs` during collect.

## Re-verify an existing CSV

Without re-cloning repos:

```bash
./truffles verify -f data/remote-results-20261011-052133/results.csv
# → results-verified.csv (or -out path)
```

Flags: `-workers`, `-detectors`, `-limit`, `-bin`

## Combine old worker CSVs manually

Secret-dedup (prefer `verified=true`, then `https://github.com/` URLs):

```bash
./truffles combine data/remote-results-20261011-052133 -o data/remote-results-20261011-052133/results-combined.csv
```

## Consolidate everything under `data/`

One command builds a canonical rollup in `data/consolidated/`:

```bash
./truffles consolidate
./truffles consolidate --update-master --update-clean

# After rollup, delete per-run remote-results* dirs (keeps data/consolidated + data/jobs + master lists):
./truffles consolidate --update-master --update-clean --purge --yes
```

| Output | Meaning |
|--------|---------|
| `results-all.csv` | All unique secrets (deduped) from every `results*.csv` under `data/` |
| `results-all-verified.csv` | `verified=true` rows only |
| `scanned-repos.txt` | Merged scanned URLs from `data/scanned-repos.txt` + all `scanned-*.txt` |
| `manifest.txt` | List of `remote-results*` run dirs |

`--update-master` also writes merged URLs to `data/scanned-repos.txt` (playbook skip source). `--update-clean` runs `truffles clean rebuild --include-scanned`.

## Low verified rate

Common causes:

- Placeholders, docs, revoked keys in git history
- `-since 2y` skipping old leaks
- Noise from broad prefixes (`"openai" in:file`)
- Live verification failing (dead keys)

Tighten prefixes (`examples/prefixes-openai.txt`), use [Skip lists and clean repos](Skip-Lists-and-Clean-Repos), and run `verify` on merged CSV.

## Security

CSV files contain **plaintext secrets**. Keep under `data/` (gitignored). Do not commit or paste into tickets.
