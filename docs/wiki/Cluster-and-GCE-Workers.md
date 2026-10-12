# Cluster and GCE workers

Cluster commands distribute a repo list across SSH workers defined in the playbook `slaves:` section.

## Lifecycle

```bash
# Check SSH + paths
./truffles cluster status -c remote-playbook.yaml

# Push linux amd64 binary to workers
./truffles cluster deploy -c remote-playbook.yaml

# Split repo list and start remote scans
./truffles cluster run -c remote-playbook.yaml \
  -f data/repos-loop-20261011-185522.txt \
  -out remote-results-20261011-185522 \
  -data-dir data -y

# Pull CSVs and merge → results.csv + results-verified.csv
./truffles cluster collect -c remote-playbook.yaml \
  -out remote-results-20261011-185522 \
  -data-dir data -y

# Kill remote trufflehog processes
./truffles cluster stop -c remote-playbook.yaml
```

`cluster run` shuffles repos before split so heavy clones spread across workers.

After **collect**, workers auto-purge copied artifacts. Mid-scan, clones under `/tmp/truffles-since-*` can fill the disk — scans **stall at ≥ 90%** until space is freed.

## Disk clean

```bash
# Safe mid-scan: only orphan /tmp/truffles-since-* dirs
./truffles cluster clean-disk -c remote-playbook.yaml --orphans-only

# Preview
./truffles cluster clean-disk -c remote-playbook.yaml --dry-run

# Full purge of workdir artifacts (after collect / when idle)
./truffles cluster clean-disk -c remote-playbook.yaml
```

`job status` prints a **Clean Disk** / **Grow Disk** tip when a worker is near full.

## Scale concurrency or machine type

```bash
# Next scan cycle uses workers: 4 on every slave (YAML updated)
./truffles cluster scale -c remote-playbook.yaml --workers 4

# Resize selected GCE VMs (stop → set-machine-type → start; interrupts that worker)
./truffles cluster scale -c remote-playbook.yaml \
  --worker worker3,worker4 --machine e2-standard-2 --workers 4
```

Prefer `--workers` alone while a job is running if you cannot interrupt clones. `--machine` needs gcloud auth (human account preferred over Firebase SA).

## GCE management

```bash
./scripts/manage-gce-workers.sh YOUR_PROJECT suggest
./scripts/manage-gce-workers.sh YOUR_PROJECT list
./scripts/manage-gce-workers.sh YOUR_PROJECT create --count 2 --machine e2-standard-2
```

Set `gce_instance`, `gce_zone`, `gce_project` per slave in `remote.env` / playbook for auto-heal during long runs.

## When stopping a job

`truffles job stop` kills the **local** loop. Always run **`cluster stop`** afterward if workers might still be scanning (see [Jobs and the 24/7 loop](Jobs-Continuous-Loop)).

## Collect flags

- `-out` — scan directory name under `-data-dir` (not `-o`)
- `-y` — non-interactive

During run, partial `results-worker*.csv` may appear on the coordinator via mirror pull — collect merges them at the end.
