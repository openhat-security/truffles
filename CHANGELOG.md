## [Unreleased]

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
