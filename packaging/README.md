# Packaging

Release channels for truffles. Tagged releases (`v*`) are built by GoReleaser
(`.goreleaser.yaml` + `.github/workflows/release.yml`).

| Channel | How users install | Source |
|--------|-------------------|--------|
| GitHub Release | `scripts/install.sh` | bare binaries + `.deb` / `.rpm` |
| Homebrew | `brew install --cask openhat-security/tap/truffles` | `openhat-security/homebrew-tap` (Cask) |
| apt | `curl …/install-apt-truffles.sh \| sudo bash` | Pages repo `openhat-security/packages` (shared pool) |
| dnf | `curl …/install-dnf-truffles.sh \| sudo bash` | same Pages repo |
| Scoop | `scoop bucket add openhat https://github.com/openhat-security/scoop-bucket` then `scoop install truffles` | `openhat-security/scoop-bucket` |
| winget | `winget install OpenHatSecurity.Truffles` (after PR merges) | [`winget/`](winget/) |
| npm | `npm i -g truffles` (name unclaimed on npm as of 2026-10) | [`npm/`](npm/) |

## Layout

```
packaging/
  npm/           npm wrapper (postinstall downloads GitHub Release binary)
  winget/        winget manifest generator + per-version output
  repo/          apt/dnf Pages templates + build-pages-repos.sh (multi-product)
```

For a new Go CLI, start from the org template:
[openhat-security/go-cli-packaging-template](https://github.com/openhat-security/go-cli-packaging-template).

## Shared `openhat-security/packages` (apt/dnf)

The Pages site hosts **one deb pool** and **one rpm tree** for all OpenHat CLIs.
Each product adds its `.deb`/`.rpm` on release and publishes product-specific
list/repo files and `install-apt-<product>.sh` scripts.

`packaging/repo/build-pages-repos.sh --merge --product truffles …` copies new
packages into the existing tree and rebuilds `Packages` / `repodata` without
removing runhug (or other) artifacts. See [repo/build-pages-repos.sh](repo/build-pages-repos.sh).

**Important:** every product that publishes to `openhat-security/packages` must use
`--merge --product <name>`. Workflows that delete the whole site except `README.md`
and copy only one product's tree will remove other products' debs and install scripts.

Truffles ships the generalized `packaging/repo/` tree (runhug + truffles product templates).

## Org repos (create once per org)

```bash
gh repo create openhat-security/homebrew-tap --public --description "Homebrew tap for OpenHat Security CLIs" --add-readme
gh repo create openhat-security/scoop-bucket --public --description "Scoop bucket for OpenHat Security CLIs" --add-readme
gh repo create openhat-security/packages --public --description "apt + dnf repos for OpenHat Security (GitHub Pages)" --add-readme
# Settings → Pages → Deploy from branch main / (root)
```

## Secrets (on `openhat-security/truffles`)

| Secret | Required for |
|--------|----------------|
| `NPM_TOKEN` | `npm publish` from `packaging/npm` |
| `PACKAGING_TOKEN` | Push `homebrew-tap`, `scoop-bucket`, and `packages` (apt/dnf Pages) |
| `HOMEBREW_TAP_TOKEN` / `SCOOP_TOKEN` | Optional overrides instead of `PACKAGING_TOKEN` |
| `WINGET_PAT` | Auto-PR to `microsoft/winget-pkgs` |
| `GPG_PRIVATE_KEY` | Optional signing for apt InRelease (unset = `trusted=yes`) |

`PACKAGING_TOKEN` should be a fine-grained PAT (or classic) with **contents: write** on `homebrew-tap`, `scoop-bucket`, and `packages`. Without it, GoReleaser still publishes the GitHub Release; brew/scoop upload is skipped (do **not** fall back to `GITHUB_TOKEN` — it cannot write other repos).

To re-publish npm for an existing tag: Actions → Release → Run workflow → set tag `vX.Y.Z` with **npm_only**.

## Cut a release

From a clean `main` (after Unreleased changelog bullets exist):

```bash
./scripts/release.sh patch
git push origin vX.Y.Z
```

Pushing `v*` runs `.github/workflows/release.yml` (GoReleaser + optional npm/winget).
When the GitHub Release is **published**, `.github/workflows/publish-linux-repos.yml`
merges `.deb`/`.rpm` into `openhat-security/packages`.

### After merge — verify channels

```bash
# GitHub Release assets
gh release view vX.Y.Z -R openhat-security/truffles

# Homebrew (after tap commit from GoReleaser)
brew update && brew install --cask openhat-security/tap/truffles

# apt (after publish-linux-repos)
curl -fsSL https://openhat-security.github.io/packages/install-apt-truffles.sh | sudo bash

# Scoop
scoop install truffles

# npm (if NPM_TOKEN configured)
npm i -g truffles
```

## Local snapshot

```bash
make release-snapshot   # if defined; else: goreleaser release --snapshot --clean
```
