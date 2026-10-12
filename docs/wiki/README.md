# Wiki source

Markdown here is the **source of truth** for the GitHub Wiki.

## Publish

```bash
chmod +x scripts/sync-wiki.sh   # once
./scripts/sync-wiki.sh
```

Live site: https://github.com/openhat-security/truffles/wiki

## Conventions

- `Home.md` is the wiki landing page.
- Use `[Link text](Page-Name)` — GitHub Wiki page titles match the filename without `.md` (hyphens become spaces in the UI).
- After editing, run `sync-wiki.sh` and commit changes in the main repo.
