# truffles wiki

Operational guide for [truffles](https://github.com/openhat-security/truffles): find GitHub repos, scan with trufflehog, verify secrets, run remote GCE clusters, and keep a 24/7 loop under control.

**Read this first:** [Jobs and the 24/7 loop](Jobs-Continuous-Loop) — how to start, monitor, stop, and restart without leaving orphan worker scans.

## Quick links

| Goal | Page |
|------|------|
| Install binary + trufflehog | [Installation and setup](Installation-and-Setup) |
| First local search + scan | [Getting started](Getting-Started) |
| `remote.env` + playbook YAML | [Remote environment](Remote-Environment) |
| OpenAI / prefix code search | [Search](Search) |
| trufflehog scan flags | [Scan](Scan) |
| `results-verified.csv` | [Verify and results](Verify-and-Results) |
| YAML playbooks | [Playbooks](Playbooks) |
| SSH workers, GCE, collect | [Cluster and GCE workers](Cluster-and-GCE-Workers) |
| **Background jobs (recommended)** | [Jobs and the 24/7 loop](Jobs-Continuous-Loop) |
| `scanned-repos.txt` / `clean-repos.txt` | [Skip lists and clean repos](Skip-Lists-and-Clean-Repos) |
| Ctrl+C and artifact paths | [Interrupts and artifacts](Interrupts-and-Artifacts) |
| Coordinated disclosure | [Disclose](Disclose) |
| 403 proxies, stuck workers | [Troubleshooting](Troubleshooting) |
| All flags | [CLI reference](CLI-Reference) |

## Typical remote OpenAI pipeline

1. Copy [remote.example.env](https://github.com/openhat-security/truffles/blob/main/remote.example.env) → `remote.env` (gitignored).
2. Point [remote-playbook.yaml](https://github.com/openhat-security/truffles/blob/main/remote-playbook.yaml) at your workers and tokens.
3. Build: `go build -o ./truffles ./cmd/truffles`
4. Start one background job (not tmux + manual loop):

```bash
./truffles job start -c remote-playbook.yaml --prefix-file examples/prefixes-openai.txt
```

5. Watch: `./truffles job list` → `./truffles job logs <id> -f`
6. After each cycle, open `data/remote-results-<timestamp>/results-verified.csv`.

## Repo docs (non-wiki)

- [README](https://github.com/openhat-security/truffles/blob/main/README.md) — install + quick start
- [docs/architecture.md](https://github.com/openhat-security/truffles/blob/main/docs/architecture.md) — proxy pool, durability
- [docs/demos.md](https://github.com/openhat-security/truffles/blob/main/docs/demos.md) — VHS demo tapes

## Maintaining this wiki

Wiki pages are authored in the main repo under `docs/wiki/`. Publish to GitHub with:

```bash
./scripts/sync-wiki.sh
```

Requires push access to `openhat-security/truffles.wiki`.
