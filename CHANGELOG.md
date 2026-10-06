## [Unreleased]

### Added
- ASCII banner, grouped short help, and `truffles help full` flag reference
- "Next:" hints after a successful `search`
- Landing README with hero SVG, `-help` screenshot, wizard docs, and VHS demos against `openhat-security`
- `make demo-vhs` / `demo-vhs-all` (Charm VHS tapes under `assets/`)

### Fixed
- `-exclude-paths` now maps to trufflehog `--exclude-globs` (was incorrectly passed as a file path)

## [1.0.0] - 2026-10-06
### Added
- Initial release of truffles: search and scan with trufflehog
