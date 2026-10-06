# Examples

Examples for using truffles.

## Playbook

Run with:
```bash
./truffles playbook -f examples/playbook.sample.yaml
```

Or in background:
```bash
./truffles playbook -f examples/playbook.sample.yaml -d -pidfile /tmp/truffles.pb.pid
```

## Shell scripts

From repo root:

```bash
# Global search + scan
./examples/search-scan.sh "llm"

# Owner enumeration + scan (with optional patterns)
./examples/owner-scan.sh BurntSushi '*llm*'
./examples/owner-scan.sh BurntSushi
```

Both scripts take a string as the first arg (or sensible defaults) and perform search then scan.
