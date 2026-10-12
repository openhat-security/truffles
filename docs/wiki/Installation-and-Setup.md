# Installation and setup

## Install truffles

See the [README install section](https://github.com/openhat-security/truffles/blob/main/README.md#install) for Homebrew, apt, dnf, scoop, winget, npm, and curl installer.

From source (development):

```bash
git clone https://github.com/openhat-security/truffles.git
cd truffles
make build    # → ./truffles
```

## Install trufflehog

Required for `scan`, `playbook`, and cluster scans on workers:

```bash
brew install trufflesecurity/trufflehog/trufflehog
# or see https://github.com/trufflesecurity/trufflehog#installation
```

Override path: `truffles scan -bin /path/to/trufflehog`

## GitHub tokens

Create one or more fine-grained or classic PATs with **public repo** read access (and private if you scan private repos).

For local runs:

```bash
export GITHUB_TOKEN=ghp_...
export GITHUB_TOKEN_2=ghp_...   # optional second token; search round-robins
```

For remote playbooks, put tokens in `remote.env` (see [Remote environment](Remote-Environment)). **Never commit** `remote.env`.

## Verify install

```bash
./truffles version
trufflehog --version
./truffles cluster status -c remote-playbook.yaml   # if using remote workers
```

## CI parity

```bash
make check
```

Same checks as GitHub Actions.
