# Demos

Recorded with [VHS](https://github.com/charmbracelet/vhs). Local tapes hit `openhat-security`; **remote / cluster / job** tapes use the live GCE fleet via `remote-playbook.yaml` + `remote.env` (status / dry-run only — they do not start or stop jobs).

```bash
brew install vhs
make demo-vhs TAPE=quickstart   # README hero
make demo-vhs TAPE=remote       # remote-playbook + fleet
make demo-vhs-all               # every tape under assets/
```

## Gallery

<p>
<img src="../assets/screenshots/search.gif" alt="full org search" width="49%"/>
<img src="../assets/screenshots/filter.gif" alt="glob filter *run*" width="49%"/>
</p>

<p>
<img src="../assets/screenshots/scan.gif" alt="search then scan" width="49%"/>
<img src="../assets/screenshots/playbook.gif" alt="vim playbook + run" width="49%"/>
</p>

<p>
<img src="../assets/screenshots/remote.gif" alt="remote-playbook + GCE fleet" width="49%"/>
<img src="../assets/screenshots/job.gif" alt="job status on live fleet" width="49%"/>
</p>

<p>
<img src="../assets/screenshots/quickstart.gif" alt="full quickstart reel" width="100%"/>
</p>

| Tape | Story |
| --- | --- |
| `help` | Banner, `examples`, `version` |
| `search` | Enumerate every public repo under `openhat-security` |
| `filter` | Glob filter `*run*` on that org |
| `scan` | Search `*run*` then `scan` with trufflehog |
| `playbook` | Author YAML in vim, then `playbook -f` |
| `playbook-gen` | `playbook:touch` + `playbook:gen` skeletons |
| `verify` | Live-verify secrets from a fixture CSV |
| `disclose` | Disclose dry-run (no `-submit`) |
| `results` | `combine` secret-dedup + `consolidate` help |
| `remote` | `remote-playbook.yaml`, `cluster status`, `job status`, clean-disk dry-run |
| `cluster` | Cluster help, live status, clean-disk dry-run, scale help |
| `job` | `job list` / `status` / `--full` / `logs` on the running loop |
| `quickstart` | Search → filter → playbook → remote tip (README hero) |

Fixtures (fake secrets only): [`assets/fixtures/`](../assets/fixtures/).
