# Examples

## Playbook

```bash
./truffles playbook -f examples/playbook.sample.yaml
# background:
./truffles playbook -f examples/playbook.sample.yaml -d -pidfile /tmp/truffles.pb.pid
```

The sample playbook enumerates `openhat-security` with the `*run*` glob, then scans.

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

# Owner enumeration + scan (with optional patterns)
./examples/owner-scan.sh openhat-security '*run*'
./examples/owner-scan.sh openhat-security
```
