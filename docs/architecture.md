# Architecture

Why this tool looks the way it does. Most of it comes down to one constraint:

> GitHub rate-limits **per source IP**: 60 requests/hour for core, 10/minute for
> search. One machine has exactly one budget, so a naive enumerator stalls.

## Layout

| Path | Role |
|---|---|
| `cmd/truffles/main.go` | entry point; maps an error to exit status 1 |
| `internal/cli/cli.go` | dispatch, usage text, interspersed flag parser |
| `internal/cli/search.go` | repo discovery: owner enumeration + global queries, pagination, workers, retries |
| `internal/cli/scan.go` | reads a URL file, runs trufflehog concurrently, owns report durability |
| `internal/cli/report.go` | `Finding`, trufflehog output parsing, pretty/csv/jsonl rendering |
| `internal/cli/style.go` | colour palette and TTY detection |
| `internal/proxy/pool.go` | proxy candidate sources, validation, replenishment, per-IP pacing, selection |

`Main(args []string) error` rather than reading `os.Args` and calling `os.Exit`,
so the dispatcher is testable and exit codes stay the entry point's business.
Everything is under `internal/`: this is a command, not a library.

---

## 1. The proxy pool

### What it was

The first version grabbed 10 proxies from a single free-proxy list that only
served `country=us`, and used them without testing:

```go
proxies := getFreeProxyList()   // 10 entries, unvalidated
for _, p := range proxies {     // used blind
    client := &http.Client{Transport: &http.Transport{Proxy: proxyURL(p)}}
}
```

**Measured yield: 0 of 10 worked.** The run fell back to a single direct IP and
was rate-limited within minutes.

### What it is now

**Candidates come from 3 sources across all countries (~4000 after dedupe).**

- `api.proxyscrape.com/v4/free-proxy-list` (`country=all`)
- `TheSpeedX/PROXY-List`
- `clarketm/proxy-list`

Each candidate is validated by actually fetching `https://api.github.com/zen` —
the smallest possible GitHub endpoint, returning `"Design for failure."`. A
proxy is only admitted if that returns HTTP 200 over TLS. Measuring against the
real endpoint matters: a proxy that can open a socket is not necessarily a proxy
that can complete a TLS handshake with GitHub.

**Measured validation yield: ~4.1–4.7%.** That is simply what free proxies are
worth. The design accepts it rather than pretending otherwise.

**Candidates are banked the moment they validate**, not after a batch drains.
Most candidates die slowly — a typical failure breakdown is `timeout=66%
refused=13% other=13% eof=4% reset=2%`. Since timeouts dominate, waiting for a
whole batch to finish meant holding an empty pool for ~40s even when working
proxies had already been found. Banking incrementally lets the first success
unblock the run immediately.

**The pool replenishes continuously.** ~58% of a working pool is dead within 45
seconds, so a one-shot fetch at startup is useless. A background goroutine tops
the pool back up whenever it drops below target.

### The local machine is in the pool too

`-use-direct` (on by default) seeds the pool with this machine's own IP as a
member under the same pacing rules. It is deliberately a **filler**, not a peer:

- It is chosen only when it is *strictly* available sooner than the best proxy.
  On a tie the proxy wins. If direct won ties it took 9 of 10 pages, which spends
  the one budget that cannot be replaced — an exhausted free proxy is free, a 403
  on the user's IP also breaks their other GitHub work.
- A failing direct member backs off 90s instead of being retired. The local IP
  does not "die"; marking it broken would remove the only guaranteed source.
- Because it is seeded immediately, `Stats.Alive` is nonzero at construction.
  Readiness therefore checks `Stats.ProxiesAlive`, which excludes it — otherwise
  the pool looks ready before a single proxy validates and `-pool-wait` never
  waits, and every early page silently goes out over the local IP.

### Why pacing matters more than proxy count

Adding proxies without pacing does not help: each IP still has its own budget,
and the failure mode is a wall of `403`s spread across dead proxies. So every
proxy carries its own next-allowed-request time, and `Next()` hands out the
proxy with the earliest availability:

- `coreInterval = 60s` — repo enumeration, matching the 60/hour budget
- `searchInterval = 6s` — search, matching the 10/minute budget

This is what actually distributes load across IPs instead of hammering the
fastest one.

### Why probes run wide (300 concurrent)

Validation yield is **flat** with concurrency, but wall time is not. Same 400
candidates, same yield:

| `-probe-par` | kept | wall time |
|---|---|---|
| 60 | 21 | 39s |
| 150 | 22 | 21s |
| 300 | 21 | 13s |

~5% either way, so concurrency is nearly free — probing is outbound TCP to
random hosts, not GitHub traffic. The default went from 60 to 300, which is what
turned "0 proxies validated in 60s" into "30/30 validated in ~3s".

A TCP pre-filter was measured too: it weeds 200 candidates down to 124 for the
same number of keepers. Worth ~15% throughput, so it was left out rather than
adding a code path for a modest gain.

### Proxies are process-scoped

The pool is threaded through explicitly as an `*http.Client` per request. It
never sets `HTTP_PROXY`/`HTTPS_PROXY`, so the tool cannot leak proxy config into
the calling shell or sibling tools. If the pool produces nothing, the run says
so loudly and goes direct.

---

## 2. Pagination: never waste a request

The first version had workers speculatively grab pages, so a 92-result search
issued **8 calls** — burning the exact budget this tool exists to conserve.

Two GitHub behaviors make exact scheduling possible:

- **Owner enumeration** — the `Link: rel="last"` header states the total page count.
- **Global search** — the response body has `total_count`.

So page 1 is fetched, the exact page count is computed, and *only those* pages
are queued:

```
calls: 1, 2, 1   (was 8)
```

Global search is capped at **10 pages** (`maxSearchPages`), because GitHub only
exposes the first 1000 results. Page 11+ returns
`422 "Only the first 1000 search results are available"`, which is handled as a
truncation signal, not a fatal error.

---

## 3. Failures are never silent

Two bugs during testing lost data without failing loudly.

### 31 repos missing (5-org run: 701 of 732)

A page whose proxy died mid-request was dropped with a single log line. The
search still "succeeded" — it was just wrong.

Now both discovery *and* page fetching retry across 3 rounds, each round with
freshly validated proxies. Anything still failing is reported:

```
INCOMPLETE — 2 page(s) failed:
  rust-lang p3
  nodejs p7
```

Current state: **732/732**.

### A deadlock on any source over 256 pages

The job producer pushed every queued page into a 256-buffered channel *before
starting any worker*. With more pages than buffer capacity, it blocked forever.
A global search easily queues 1000, so `search llm` simply hung.

Fixed by feeding from a separate goroutine that selects on the context, and by
having workers stop pulling once `-limit` is satisfied.

---

## 4. Search mode semantics

Two distinct modes, because "pattern" means different things for each:

| Invocation | Positionals are | Filtering |
|---|---|---|
| `-owner X` + patterns | local filters | union match on repo name |
| no `-owner` | **search queries** | none; the index already filtered |

Treating global positionals as local filters would have been subtly wrong:
`search walkdir` would only match repos named *exactly* `walkdir`, returning 13
of 92 results. As a query it returns all 92. `-filter` opts back into local
filtering.

Each query becomes its own `srcSpec`, which is what lets multiple queries fan
out across workers and distinct proxies — the case that made `-workers 8` a no-op
when there was only one page of work to do.

---

## 5. Progress reporting

Both subcommands used to be silent until they finished. That is a real
usability failure, not a cosmetic one: a 90-second search and a hung search
produced byte-identical output, and a 100-repo scan prints one header line and
then nothing for however long the clones take (measured: `vercel/ai` alone took
2m26s).

Both now report continuously, to **stderr**, so reports on stdout stay clean:

- **search** — a line every ~2s as repos are found, plus a heartbeat that
  distinguishes "no new results yet" from a hang. Reports are throttled because
  one API page can yield 100 repos and unthrottled logging produced a burst of
  identical lines.
- **scan** — one status line per repo on completion (duration + finding count)
  and a heartbeat with done/failed/in-flight counts, elapsed and ETA.

Counters are `atomic.Int64` so the heartbeat goroutine can read them without
racing the workers; verified under `go build -race`.

`scan` captures trufflehog's stdout (findings) and stderr (its log flood)
separately, which is what makes an accurate per-repo finding count possible and
keeps the noise off the console unless `-v` is passed.

### Report durability and formats

The report defaults to `<inputstem>-<unixtimestamp>.<ext>` so runs never
overwrite each other, and is **flushed and fsynced after every repo**. A
100-repo scan is ~20 minutes; losing it to a stray Ctrl-C would be worse than
the scan itself. `SIGINT`/`SIGTERM` are caught, the report is flushed, an
`=== Interrupted ===` marker is appended, and the process exits 130 — so a
partial report is never mistaken for a complete one.

That durability requirement is what dictated the machine-readable formats:

- **`pretty`** — colourised sections with `VERIFIED`/`UNVERIFIED` badges,
  `file:line`, short commit, author, and a per-detector summary. Prose is fine
  here, so this is the only format that records clean and failed repos.
- **`csv`** — one row per finding via `encoding/csv`. Not hand-rolled: secrets
  contain commas, quotes and newlines, and the verification errors contain
  escaped quotes.
- **`jsonl`** — one JSON object per finding. JSON *Lines* rather than a single
  array specifically so the file remains valid and parseable after every repo.

For `csv` and `jsonl`, the human summary is written to stderr instead of the
file, since appending prose would corrupt the data. Verified: both parse
cleanly, and all three formats are race-free under `-race`.

## 6. Output

`repos.txt` is streamed and flushed every 10 lines, so a killed or rate-limited
run still leaves a usable partial file. Matching is a `map[string]bool` dedupe on
`owner/name`, which is what makes concurrent workers safe to emit from
concurrently.

## 7. Bugs worth remembering

Both of these were found by measuring rather than reading, and both are the kind
of failure that looks like success.

**Unclonable repos reported as clean.** `res.Err` was never assigned, so every
failure path rendered through dead code and a repo that could not be cloned was
reported `[ok] clean`. For a scanner this is the worst class of bug: a private
repo scanned without a token was indistinguishable from a repo with no secrets.
`scanError` now extracts the reason from trufflehog's JSON stderr logs, and
permanent failures (repository not found, auth) are not retried.

**`--` terminator dropped.** The interspersed flag parser reorders arguments to
put flags first, which silently discarded `--`, so `truffles search -- -weird`
failed with "flag provided but not defined". The terminator is now carried
through the reordering.

## Known limitations

- **Global search is not exhaustive.** GitHub caps it at 1000 results per query
  and ranks by relevance. Owner enumeration is the exhaustive path.
- **Free proxies are genuinely unreliable.** ~4% yield with 58% 45-second
  half-life. `-token` is strictly better; the pool is for when you have none.
- **Scan throughput is clone-bound.** ~90% of a scan is
  `git clone --mirror` of the full history. `-workers` past 4 buys nothing
  (measured 5m22s at `-w 4` vs 5m20s at `-w 12`) and mainly multiplies peak disk
  usage. trufflehog exposes no shallow or treeless clone, so the only way past
  it is giving up history.
- **Test coverage is partial.** Parsing, reporting, flag handling, proxy
  selection and error extraction are unit tested offline; the orchestration and
  retry paths around live GitHub and trufflehog are still verified empirically.
- **No resume/checkpoint.** A killed run restarts from scratch; partial output is
  preserved but not resumed.