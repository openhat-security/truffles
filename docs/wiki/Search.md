# Search

`truffles search` discovers GitHub repository URLs and writes one URL per line.

## Modes

| Mode | How |
|------|-----|
| Owner enumeration | `-owner org/user` then optional glob patterns |
| GitHub search API | `-q 'query'` (repeatable) |
| **Prefix code search** | `-prefix-file file.txt` — each line becomes `"<prefix>" in:file` |

Prefix mode is what [Jobs and the 24/7 loop](Jobs-Continuous-Loop) uses for OpenAI keys (`examples/prefixes-openai.txt`) or broad provider lists (`examples/prefixes.txt`).

**Terms file** (`-terms-file`) is the same idea with a clearer name: one term per line, optional full GitHub queries. Example: `examples/search-terms.example.txt`.

**Shuffle seed** (`-seed`): GitHub always returns the same ordering for a query, so reruns used to walk page 1 first and burn quota on repos already in `scanned-repos.txt`. With `-seed`, truffles shuffles which **queries** and **pages** (1–10) are fetched first. `-seed 0` picks a new random seed each run (printed in logs); use a fixed seed to replay.

## Skip already-seen repos

| Flag | File | Effect |
|------|------|--------|
| `-skip-file` | `data/scanned-repos.txt` | Skip URLs already scanned |
| `-clean-file` | `data/clean-repos.txt` | Skip repos with zero verified secrets |

Skipped repos **do not count** toward `-limit`.

## Rate limits and proxies

- Pass `-token` or set `GITHUB_TOKEN` — required for code search (401 without)
- Default: proxy pool (~30 validated proxies) + optional direct traffic (`-use-direct`, default true)
- **Your proxies:** `-proxy-file examples/proxies.example.txt` — your `host:port` lines are **probed before** scraped public lists; prefix `trusted:` to skip probe (paid/static proxies)
- Playbook: `search.proxy_file: path/to/proxies.txt`
- `-no-proxy` — this machine only
- `-no-direct` — proxies only

Verbose proxy per request: `-v`

## Example: OpenAI-focused discovery

```bash
./truffles search \
  -prefix-file examples/prefixes-openai.txt \
  -skip-file data/scanned-repos.txt \
  -clean-file data/clean-repos.txt \
  -limit 500 \
  -workers 8 \
  -token "$GITHUB_TOKEN,$GITHUB_TOKEN_2" \
  -out data/repos-batch.txt
```

## Output

`-out repos.txt` or stdout. Each line is a `https://github.com/owner/repo` URL.

See [CLI reference](CLI-Reference) for full flags.
