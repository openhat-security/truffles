# Disclose

`truffles disclose` opens coordinated disclosure from scan CSVs (dry-run by default).

```bash
./truffles disclose -f data/remote-results/results.csv
./truffles disclose -f data/remote-results -submit
```

## Requirements

- Bot token: `-token` or `OPENHAT_BOT_GH_TOKEN` (often via `-env-file remote.env`)
- Verified findings by default; `-all` includes unverified

## Behavior

- Private vulnerability reporting enabled → private advisory
- Otherwise → public issue with contact email (no secret in issue body by default)

Use `-include-secret` only for private advisory flows.

**Never commit** tokens or CSVs with live secrets.
