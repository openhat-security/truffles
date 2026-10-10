# Examples

## Playbook

```bash
./truffles playbook -f examples/playbook.sample.yaml
# background:
./truffles playbook -f examples/playbook.sample.yaml -d -pidfile /tmp/truffles.pb.pid
```

The sample playbook enumerates `openhat-security` with the `*run*` glob, then scans.

**Multi-host scan parallelism** (repos split across GCE workers; SSH scans run concurrently):

```bash
./scripts/manage-gce-workers.sh sandbox420 suggest
./scripts/manage-gce-workers.sh sandbox420 create --count 2 --machine e2-standard-2
./scripts/manage-gce-workers.sh sandbox420 env truffles-worker-1 1 >> examples/remote-multi.env
./scripts/manage-gce-workers.sh sandbox420 env truffles-worker-2 2 >> examples/remote-multi.env
./truffles cluster deploy -c examples/playbook.remote-multi.yaml
./truffles playbook -f examples/playbook.remote-multi.yaml
```

Author your own:

```bash
vim playbook.yaml
./truffles playbook -f playbook.yaml
```

## Shell scripts

From repo root:

```bash
# Global search + scan
./examples/search-scan.sh "llm"

# Prefix-driven GitHub *code* search (then scan)
./truffles search -prefix-file examples/prefixes.txt -limit 500 -out repos.txt
./truffles search -prefix-file examples/prefixes-providers.example.txt -limit 300 -out provider-repos.txt

# Owner enumeration + scan (with optional patterns)
./examples/owner-scan.sh openhat-security '*run*'
./examples/owner-scan.sh openhat-security
```
