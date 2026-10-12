# Troubleshooting

## Search: `rate limited (403)`

GitHub code search quota exhausted for that IP/token. Mitigations:

- Add `GITHUB_TOKEN` / second token in `remote.env`
- Wait for cooldown; proxies rotate automatically
- Narrow `-prefix-file` (avoid huge `"openai" in:file` unless needed)
- Reduce `-workers` if many parallel 403s

Message `pass -token to raise the limit` means unauthenticated or exhausted token on that route.

## Search: proxy timeouts

Free proxies flake. Normal — retries continue. If stuck at 0 repos for many minutes, check token in `remote.env` and network.

## Worker missing `-clean-file` / old binary

Redeploy:

```bash
go build -o ./truffles ./cmd/truffles
./truffles cluster deploy -c remote-playbook.yaml
```

## Worker stuck / OOM / SSH hang

```bash
./truffles cluster heal -c remote-playbook.yaml
```

Or manual: `cluster stop` → `cluster reset` → `cluster deploy`.

## Hung clone on one repo

Kill worker scan, add repo to skip list or `clean-repos` after manual mark, redeploy.

## Job shows running but pid dead

```bash
./truffles job list   # refresh marks exited
```

Start a new job; do not reuse old id.

## Laptop sleep paused local search

Use **`truffles job start`** (caffeinate on macOS) or keep lid open. GCE workers unaffected.

## gcloud permission errors on heal/collect

Ensure your user can `gcloud compute instances reset` for worker instances and SCP keys match `remote.env`.

## Empty collect directory

Wrong `-out` name — must match the timestamped `remote-results-*` from the cycle log, not `collect_out` default alone.

## Token visible in `ps`

Prefer env file over `-token` on CLI; rotate if logged.
