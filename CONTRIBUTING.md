# Contributing

Thanks for helping. Prefer the standard library; the only allowed third-party
import today is `gopkg.in/yaml.v3` for playbooks/cluster YAML.

## Getting started

```bash
make build     # build ./truffles (repo root)
make check     # gofmt check, go vet, go test
make test      # tests only
make race      # tests under the race detector (CI also runs -race)
```

## Ground rules

**Minimal deps.** Adding a dependency changes the supply-chain story for every
user of a security tool. That trade has to be earned.

**Tests must not touch the network.** Tests cover parsing, reporting, flag
parsing, proxy selection and error handling — all pure logic. Anything that
shells out to `trufflehog` or calls GitHub needs an interface seam and a fake,
not a live call.

**Never commit a report.** Scan output contains live credentials. The
`.gitignore` covers `<name>-<timestamp>.txt`, `*.csv` and `*.jsonl`; if you
find a way to leak one, that is a bug worth reporting.

**Measure before claiming a speedup.** Performance notes live in
[docs/architecture.md](docs/architecture.md). If you change a code path,
re-measure and update the notes, or remove the claim.

## Pull requests

- One logical change per PR.
- `make check` passes.
- New behaviour comes with a test.
- Commit messages say why, not what:

  ```
  fix: stop reporting unclonable repos as clean

  res.Err was never assigned, so every failure path rendered as
  "[ok] clean". A private repo scanned without a token was
  indistinguishable from a repo with no secrets.

  Fixes #12
  ```

- Update `README.md` if you change flags, output or defaults.

## Reporting bugs

Open an issue with:

- what you ran, verbatim
- what you expected
- what happened, including the `truffles scan` status lines (`[ok]`/`[++]`/`[!!]`)
- `-v` output if the proxy pool is involved

**Do not paste report contents.** They contain live secrets. A status line or a
redacted finding is enough.

If you have found a leaked credential, report it to whoever owns the affected
repository rather than publishing it here.
