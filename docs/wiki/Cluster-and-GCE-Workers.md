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
