# Examples

Run scripts from the **repo root** (`./examples/...`).

## AI key discovery (search only)

Shuffled GitHub code search for repos mentioning known AI/LLM key patterns. Skips repos already in `data/scanned-repos.txt` and `data/clean-repos.txt`.

```bash
go build -o ./truffles ./cmd/truffles
source remote.env   # or export GITHUB_TOKEN

./examples/search-ai-keys.sh
# LIMIT=300 PREFIX_FILE=examples/prefixes.txt ./examples/search-ai-keys.sh  # all providers
```

Output: `data/repos-ai-<timestamp>.txt` plus `data/performance.json` (page-level timings). Core events: `tail -f data/truffles.log`.

**Next (remote GCE):**

```bash
./truffles cluster run -c remote-playbook.yaml -f data/repos-ai-<timestamp>.txt \
  -out remote-results-$(date +%Y%m%d-%H%M%S) -data-dir data -y
```

**Next (24/7 loop):**

```bash
./truffles job start -c remote-playbook.yaml --prefix-file examples/prefixes-openai.txt --seed 0
```

### Prefix lists

| File | Use |
|------|-----|
| `prefixes-openai.txt` | OpenAI-focused (default for `search-ai-keys.sh`) |
| `prefixes.txt` | Multi-provider (OpenAI, Anthropic, AWS, Slack, …) |

## Owner enumeration

```bash
./truffles search -owner openhat-security '*run*' -out repos.txt
./truffles scan -f repos.txt
```

## Local playbook

```bash
./truffles playbook -f examples/playbook.sample.yaml
```

## Remote multi-worker (GCE)

```bash
./scripts/manage-gce-workers.sh YOUR_PROJECT suggest
./truffles cluster deploy -c examples/playbook.remote-multi.yaml
./truffles playbook -f examples/playbook.remote-multi.yaml
```

See `examples/remote-multi.example.env` for worker env vars.

## Cluster CLI (SSH)

```bash
./truffles cluster run -c examples/cluster.yaml -f repos.txt -y
```

## Results rollup

```bash
./truffles combine data/remote-results-* -o data/results-combined.csv
./truffles consolidate --update-master --update-clean
./truffles clean rebuild --include-scanned
```
