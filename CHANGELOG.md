## [Unreleased]

### Added
- `truffles cluster clean-disk` — purge orphan `/tmp/truffles-since-*` clones and workdir artifacts (`--orphans-only`, `--dry-run`); auto-purge after collect; scans stall when disk ≥ 90%
- `truffles cluster scale` — bump `slaves[].workers` and/or GCE machine type (`--workers`, `--machine`, `--worker name,…`)
- Rich `truffles job status` — progress table, per-worker CPU/Mem/Disk `% (GB)`, Repos with `/hr`, tips for clean/grow disk; `--full` adds lifetime totals, machine type, and cost estimate
- Expanded VHS demo gallery (`assets/*.tape`): scan, verify, disclose, results, playbook-gen, remote/cluster/job against live GCE fleet; longer read pauses between commands
- `examples/search-ai-llm.txt` for AI/LLM term search in remote loops
- Demo fixtures under `assets/fixtures/` (fake secrets only) for verify/disclose/results tapes

### Changed
- Job status / cluster ops prefer a human gcloud `--account` when a Firebase SA would 403

## [1.1.0] - 2026-10-12

### Added
- `truffles job` continuous loop manager (`start`/`list`/`status`/`logs`/`stop`); loop runs in-process via `job run` (no source-tree scripts required for packaged binaries)
- `truffles combine` — secret-dedup merge of `results-*.csv` (prefer verified + github.com URLs)
- `truffles consolidate` — roll up `data/` into `results-all.csv`, verified sheet, scanned list, manifest; `--update-master` / `--update-clean` / `--purge`
- `truffles clean rebuild` — rebuild `clean-repos.txt` from results CSVs
- `truffles verify` — live-verify secrets from a results CSV without recloning
- `performance.json` per run (metadata + phase summary + page/repo/scp events); replaces `timing.json`
- Search `-terms-file`, `-seed` (shuffle query/page order), `-proxy-file` with `trusted:` lines
- Proxy pool reserve / 403 backoff; core event log `data/truffles.log`
- `examples/search-ai-keys.sh` + OpenAI-focused prefixes; ops wiki under `docs/wiki/`
- Interrupt path listing for local artifacts; scan default `-since 2y` (full clone, scan window)

### Changed
- `make build` writes `./truffles` (repo root)
- `job start` defaults `--bin` to this executable; detaches `job run`
- Short “Next:” hints after search (colored labels)
- Example prune: removed obsolete cluster/owner wrapper scripts and `cluster:coop`

### Removed
- `cluster:coop` / `coop` prototype
- `scripts/combine-results.sh`, `scripts/consolidate-data.sh`, `scripts/update-clean-repos.py` (folded into CLI)
- `examples/cluster.sh`, `examples/owner-scan.sh`, and other redundant example files

## [1.0.15] - 2026-10-08

### Fixed
- Release workflow checks out the tagged commit on workflow_dispatch
- GoReleaser can republish Homebrew/Scoop without re-uploading GitHub Release assets
- Linux packages publish pulls `main` before merge

## [1.0.14] - 2026-10-07

### Added
- Multi-channel install docs and packaging: Homebrew cask, Scoop, apt/dnf repo merge workflow, npm postinstall, winget CI
- `truffles disclose` for coordinated disclosure from scan CSVs (dry-run or `-submit`)
- Remote GCE cluster: deploy, stop, reset, heal, env-file expansion, auto-heal during playbook/cluster run
- Scan `-skip-file` / `-append-scanned`, default `-format csv` and direct clones (`-no-proxy`)
- `scripts/manage-gce-workers.sh` and example remote multi-host playbook

### Changed
- Release workflow: tag push runs GoReleaser; workflow_dispatch can npm-only republish
- `make build` writes to `./bin/truffles`

### Fixed
- `-exclude-paths` maps to trufflehog `--exclude-globs` (was incorrectly passed as a file path)

## [1.0.13] - 2026-10-06

### Added
- ASCII banner, grouped short help, and `truffles help full` flag reference
- "Next:" hints after a successful `search`
- Landing README with hero SVG, `-help` screenshot, wizard docs, and VHS demos against `openhat-security`
- `make demo-vhs` / `demo-vhs-all` (Charm VHS tapes under `assets/`)

## [1.0.0] - 2026-10-06
### Added
- Initial release of truffles: search and scan with trufflehog
