# Remote environment

Remote scanning uses a **playbook YAML** plus a **dotenv file** for secrets and SSH targets.

## Create `remote.env`

```bash
cp remote.example.env remote.env
chmod 600 remote.env
```

Fill in:

- `GITHUB_TOKEN`, `GITHUB_TOKEN_2` — search + git clone on workers
- `TRUFFLES_WORKERn_HOST`, `USER`, `KEY`, `WORKDIR`, `BIN` — SSH
- `TRUFFLES_WORKERn_GCE_*` — optional; enables auto-heal (reset VM + redeploy)

`remote-playbook.yaml` references:

```yaml
env_file: remote.env
```

Variables expand as `${VAR}` in the playbook.

## Playbook output layout

| Setting | Default | Meaning |
|---------|---------|---------|
| `data_dir` | `data` | Parent directory |
| `collect_out` | `remote-results` | Base scan name; auto-suffix `-1`, `-2` if taken |

Each run creates `data/<scan-name>/repos.txt`, worker logs, and after collect `results.csv` + `results-verified.csv`.

## Security

- `remote.env` and `data/` are gitignored
- Tokens may appear in process lists if passed on CLI — prefer env file
- Rotate tokens if exposed

## Next steps

- [Cluster and GCE workers](Cluster-and-GCE-Workers) — `manage-gce-workers.sh`, deploy
- [Jobs and the 24/7 loop](Jobs-Continuous-Loop) — continuous operation
