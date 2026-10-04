# truffles

Find GitHub repositories at scale, then scan their full git history for leaked
credentials with [trufflehog](https://github.com/trufflesecurity/trufflehog).

Two subcommands, usually run as a pipeline:

```bash
./truffles search -owner BurntSushi '*llm*' -out repos.txt   # find repos
./truffles scan -f repos.txt -format csv                     # scan them
```

`search` is fast: it enumerates or queries repositories in parallel across a pool
of proxies. `scan` is slow, because trufflehog clones and walks every commit in
history. truffles makes both observable and both interruptible.

[![CI](https://github.com/adamsiwiec/truffles/actions/workflows/ci.yml/badge.svg)](https://github.com/adamsiwiec/truffles/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.24%2B-blue)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

## Contents

- [Install](#install)
- [Requirements](#requirements)
- [Quick start](#quick-start)
- [search](#search)
- [scan](#scan)
- [Report formats](#report-formats)
- [Output and durability](#output-and-durability)
- [How search goes fast](#how-search-goes-fast)
- [Performance](#performance)
- [Project layout](#project-layout)
- [Contributing](#contributing)
- [Security](#security)
- [License](#license)

## Install

```bash
git clone https://github.com/adamsiwiec/truffles
cd truffles
make build          # produces ./truffles
make install        # or: go install ./cmd/truffles
```

No third-party Go dependencies. Everything is the standard library.

## Requirements

- Go 1.24 or newer to build.
- [`trufflehog`](https://github.com/trufflesecurity/trufflehog) on `PATH` for
  `scan` only. Override the location with `-bin`.
- A GitHub token is optional for `search` (public data works without one) and
  required for scanning private repositories.

## Quick start

```bash
# Enumerate every public repo under an owner, filtered by name pattern
./truffles search -owner BurntSushi '*llm*' -out repos.txt

# Global search instead of a single owner
./truffles search 'langchain' -limit 500

# Scan them. Reports are flushed and fsynced after every repo.
./truffles scan -f repos.txt

# Machine-readable output for pipelines
./truffles scan -f repos.txt -format jsonl -out findings.jsonl
```

Every subcommand has full help:

```bash
./truffles help
./truffles scan -h
```

## search

Two modes, chosen by whether `-owner` is present.

**Enumeration** — list every repository an owner has, then filter locally. Use
this when you know whose code you want.

```bash
./truffles search -owner BurntSushi,torvalds '*llm*' -out repos.txt
./truffles search -owner BurntSushi -regex '^ri'          # regex, not globs
```

Positionals are unioned: a repo matching any pattern is kept.

**Global search** — query GitHub's search API. Use this when you want to discover
owners you did not know about.

```bash
./truffles search 'llm' -limit 500
./truffles search -q 'stars:>100 language:python rag' -q 'vector database' \
  -filter '*embed*' -limit 2000
```

Without `-owner`, positionals are treated as GitHub queries, `-q` is repeatable,
and `-filter` optionally narrows results locally.

Notes on correctness, because these are easy to get wrong:

- Results are **deduplicated by full name**, across queries, owners and pages.
- Pagination is exact. A page past the last result is treated as truncation and
  reported, not silently swallowed.
- Pages that fail after all retries are **reported loudly** rather than being
  passed off as a complete result set.
- Each page is fetched through a different proxy, so a single IP is never
  rate-limited by consecutive requests.
- `-limit 0` means unlimited. Values above 10000 are clamped.

## scan

```bash
./truffles scan -f repos.txt -workers 8
```

Input is a text file of repository URLs, one per line. Blank lines and `#`
comments are ignored.

Per-repo outcomes are distinguished, and this matters more than it sounds:

| Status | Meaning |
| --- | --- |
| `[ok]` | scanned, no findings |
| `[++]` | scanned, findings present |
| `[!!]` | **not scanned** — clone failure, auth problem, or crash |

A repo that cannot be cloned is never counted as clean. A private repository
scanned without a token looks exactly like a repository with no secrets if you
let that slide, and that is the single most dangerous way a scanner like this
can lie to you.

The reason trufflehog gave is extracted from its JSON logs and shown inline, so
`[!!]` tells you whether to add a token, fix the URL, or retry.

## Report formats

`-format pretty` (default) is for humans: colourised `VERIFIED`/`UNVERIFIED`
badges, `file:line`, short commit, author, and a summary with per-detector
counts.

```text
[++] BuilderIO/gpt-crawler  17 finding(s) in 3.8s  all unverified

  UNVERIFIED Postgres  Postgres connection string containing credentials
      secret    postgres://2f9881cc…:sk_QZ3…@db.prisma.io:5432
      file      output/concatenated.xml:2147
      commit    5964f226  2025-10-27  Tem <temurbakhriddinov@gmail.com>
      decoder   HTML

=== Summary ===
  Repos scanned          1
  Findings               17
  Verification           17 unverified
  By detector
    Postgres                       10
```

`-format csv` and `-format jsonl` are for pipelines.

- **csv** — one row per finding, 11 columns, written with `encoding/csv` because
  real secrets and verification errors contain commas, quotes and newlines.
- **jsonl** — one JSON object per finding. JSON *Lines* rather than a single
  array, so the file stays valid after every repo completes.

Both contain **findings only**. Clean and failed repos appear in the stderr
summary instead, because appending prose would make the file unparseable. Use
`pretty` if you need every repo recorded.

Both require `-json` (the default). trufflehog's text output has no
machine-readable fields, so a machine format without `-json` would quietly
produce an empty report; truffles rejects that combination instead.

## Output and durability

Reports default to a timestamped file, so runs never overwrite each other:

```
ai-100.txt  ->  ai-100-1757001234.txt     (pretty)
              ai-100-1757001234.csv     (-format csv)
              ai-100-1757001234.jsonl   (-format jsonl)
```

- `-out <file>` chooses the path; `-out -` writes to stdout.
- Progress and status always go to **stderr**, so `> report.txt` captures only
  the report.
- The report is **flushed and fsynced after every repo**. A 100-repo scan is
  long enough that losing it to a stray Ctrl-C would be worse than the scan.
- `Ctrl-C` (`SIGINT`/`SIGTERM`) keeps everything finished so far, appends an
  `=== Interrupted ===` marker so a partial report is never mistaken for a
  complete one, and exits 130.

Colour is auto-detected, so redirected files stay plain text. `NO_COLOR` is
honoured; `-color always|never` overrides.

## How search goes fast

GitHub rate-limits by IP. One IP gets 60 requests/hour on the core API and
10/minute on search, which is not enough to enumerate anything at scale.

truffles maintains a pool of public proxies, validates each against
`api.github.com` in the background, and paces every member to its own
per-IP limit. That is what turns one IP's budget into a pool's budget.

Details that make it work in practice:

- This machine's own IP **also participates**, as a filler: it is used when no
  proxy is ready, and never preferentially. Its budget is the one that cannot
  be replaced, so a free proxy is always spent first.
- The pool is **seeded with keepers as they validate** rather than waiting for a
  batch to finish, so the first page goes out in seconds.
- Retiring a proxy closes its idle sockets instead of leaking them.
- Proxies are scoped to the truffles process. **No `HTTP_PROXY` environment
  variables are set**, so your shell and other tools are unaffected.
- `-v` reports why each proxy died and which proxy served each page.

Public proxies are hostile to this: measured yield is ~4%, and ~58% die within
45 seconds. The pool replenishes continuously and falls back to a direct
connection with a diagnostic if it cannot keep up. `-no-proxy` skips the pool
entirely.

## Performance

Search is network-bound and parallelises well. Scan is **not** dominated by
secret scanning, which is worth knowing before you optimise the wrong thing.

Measured on a 14-core Mac:

| | |
| --- | --- |
| `git clone --mirror streamlit/streamlit` | 53.9s, 1.6 GB |
| 12 repos, `-w 4`, `-max-depth 1` | 5m22s |
| 12 repos, `-w 12`, `-max-depth 1` | 5m20s |

`-max-depth 1` makes scanning nearly free, yet the total barely moves: the
full-history mirror clone is the cost. So `-workers` past a handful mostly
multiplies peak disk usage rather than finishing sooner.

```bash
./truffles scan -f repos.txt -workers 8                        # small repos
./truffles scan -f repos.txt -exclude-paths node_modules,vendor,dist,*.lock
```

Two flags that look like speedups are not:

- `-max-depth N` cuts scanning CPU about 4x, but it is **lossy**: `-max-depth
  200` found 0 of `vercel/ai`'s 7 findings, because those secrets are in old
  commits. Fine for "is this repo dirty right now", wrong if historical
  secrets matter.
- `-no-verification` is **not faster** (142s vs 126s baseline, identical CPU).
  Its purpose is avoiding outbound requests to third-party APIs, at the cost of
  every result being `UNVERIFIED`.

trufflehog has no shallow or treeless clone flag, so the full-history mirror is
unavoidable in `git` mode. Going faster means giving up history, which is a
tradeoff worth making deliberately rather than by accident.

## Project layout

```
cmd/truffles/       entry point, nothing else
internal/cli/       commands, flags, help, reporting
  cli.go              dispatch and usage
  search.go           search: enumeration, global queries, pagination
  scan.go             scan: orchestration, subprocess handling
  report.go           Finding, parsing, pretty/csv/jsonl rendering
  style.go            colour palette
internal/proxy/     proxy validation, pacing and selection
docs/architecture.md  design notes and measurements
```

`internal/` is used throughout, so nothing here is importable from outside the
module. That is deliberate: this is a command, not a library.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). `make check` runs what CI runs.

## Security

This tool finds live credentials. Treat its output accordingly:

- Reports contain secrets in plaintext. They are gitignored by default; keep
  them out of version control and out of shared filesystems.
- The default `-results verified,unknown` includes unverified findings, which
  are mostly false positives. Use `-results verified` when you want only
  confirmed-live credentials.
- Rotation, not deletion, is what actually removes a leaked secret from
  GitHub's history.

## License

MIT — see [LICENSE](LICENSE).
