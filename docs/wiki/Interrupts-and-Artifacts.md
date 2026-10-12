# Interrupts and artifacts

## Ctrl+C during cluster run or scan

Recent truffles builds print **absolute paths** to local artifacts on SIGINT (coordinator and `job run` trap).

You keep every repo that finished before interrupt — reports fsync per repo.

## Ctrl+C during `job logs -f`

Only the **log follower** exits. The job keeps running in the background.

## Stop intentionally

Use [Jobs and the 24/7 loop](Jobs-Continuous-Loop): `job stop` + `cluster stop`.

## Finding artifacts for the current cycle

Check the job log for lines like:

```
cycle 1 — cluster run 340 repos → data/remote-results-20261011-185522
```

Then:

```bash
open /path/to/truffles/data/remote-results-20261011-185522
ls /path/to/truffles/data/remote-results-20261011-185522/results-*.csv
```

Job log path:

```bash
./truffles job status <id>   # prints log: …/data/jobs/<id>/job.log
```

## Partial cycle after stop

```bash
./truffles cluster collect -c remote-playbook.yaml -out <scan-name> -data-dir data -y
```
