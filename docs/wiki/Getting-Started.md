# Getting started

## Prerequisites

- Go 1.26+ to build from source (or install via Homebrew/npm — see [Installation and setup](Installation-and-Setup))
- [`trufflehog`](https://github.com/trufflesecurity/trufflehog) on `PATH` for `scan`
- Optional: GitHub personal access token(s) for higher search rate limits and private repos

## Interactive path

```bash
./truffles wizard
```

Walks through owner vs global search, filters, repo list, and scan.

## Manual two-step path

```bash
# 1) Find repos
./truffles search -owner openhat-security -out repos.txt

# 2) Scan history
./truffles scan -f repos.txt
```

Reports default to timestamped CSV under the current directory (see `scan -out`).

## Playbook path

```bash
./truffles playbook:touch -o playbook.yaml
vim playbook.yaml
./truffles playbook -f playbook.yaml
```

## Remote / scale path

When you have GCE (or other SSH) workers configured in `remote-playbook.yaml`:

1. [Remote environment](Remote-Environment)
2. [Cluster and GCE workers](Cluster-and-GCE-Workers)
3. [Jobs and the 24/7 loop](Jobs-Continuous-Loop)

## Help

```bash
./truffles help          # grouped commands
./truffles help full     # every flag
./truffles examples
```
