# Playbooks

A playbook is YAML describing **search** (optional) and **scan**, plus optional **slaves** for remote execution.

## Generate templates

```bash
./truffles playbook:gen my-scan -o playbook.yaml
./truffles playbook:touch -o playbook.example.yaml
```

## Run locally

```bash
./truffles playbook -f remote-playbook.yaml -y -data-dir data -out my-run
```

Flags:

- `-data-dir` — parent output directory (default `data`)
- `-out` — scan folder name under data-dir
- `-y` — non-interactive
- `-d` — daemonize (legacy; prefer [Jobs](Jobs-Continuous-Loop))

## `remote-playbook.yaml`

The stock remote playbook:

- Search via `prefix_file` (overridden by job loop with `--prefix-file`)
- Scan on 7 GCE slaves with capacity-weighted repo split
- `skip_file` / `append_scanned` / `clean_file` for incremental hunting
- `auto_heal: true` — reset stuck VMs when `gce_*` is set

Read the header comments in the file for **fast scan** vs **proxy spread** profiles.

## Heal workers

```bash
./truffles cluster heal -c remote-playbook.yaml
# same as: truffles playbook heal -f remote-playbook.yaml
```

Sequence: stop remote scans → `gcloud compute instances reset` → wait SSH → deploy linux binary.

## Job loop vs full playbook

| Mode | Command | Each cycle |
|------|---------|------------|
| **split** (default) | `job start` | search + `cluster run` |
| **full-playbook** | `job start --full-playbook` | `truffles playbook` (search+scan per YAML) |

OpenAI continuous hunting usually wants **split** with `examples/prefixes-openai.txt`.
