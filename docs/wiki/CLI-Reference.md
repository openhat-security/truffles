# CLI reference

Authoritative flag list from the binary:

```bash
./truffles help          # grouped overview
./truffles help full     # all commands and flags
./truffles job help
```

## Command index

| Command | Purpose |
|---------|---------|
| `search` | Discover repo URLs |
| `scan` | trufflehog on repo list |
| `verify` | Live-check secrets in CSV |
| `disclose` | GitHub advisory/issue from CSV |
| `combine` | Secret-dedup merge of `results-*.csv` |
| `consolidate` | Roll up `data/` into `consolidated/` |
| `clean rebuild` | Rebuild `clean-repos.txt` from CSVs |
| `playbook` | YAML search + scan |
| `playbook heal` | cluster heal via playbook path |
| `playbook:gen` / `playbook:touch` | YAML templates |
| `cluster init` | Cluster YAML skeleton |
| `cluster status` | Worker SSH check |
| `cluster deploy` | SCP linux binary |
| `cluster run` | Distribute + scan |
| `cluster collect` | Pull + merge results |
| `cluster stop` | Kill remote scans |
| `cluster reset` | GCE instance reset |
| `cluster heal` | stop → reset → deploy |
| `job start` | Background 24/7 loop |
| `job run` | Foreground loop (used by start) |
| `job list` / `status` / `logs` / `stop` | Job registry |
| `wizard` | Interactive setup |
| `examples` | Copy-paste samples |
| `version` | Build version |

## Common mistakes

| Wrong | Right |
|-------|-------|
| `cluster run -o name` | `cluster run -out name` |
| `job status j-YYYYMMDD-HHMMSS` | Real id from `job list` |
| `job logs` + `job stop` in one paste while debugging | Run `logs -f` alone; stop separately |
| Two concurrent `job start` | Stop old job first |

Wiki how-to pages expand the workflows; this page is the index.
