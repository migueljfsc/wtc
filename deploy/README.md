# deploy/ — running wtc

Full guide: [../docs/setup/deploy.md](../docs/setup/deploy.md). Image:
`ghcr.io/migueljfsc/wtc` (`latest` + `sha-<sha7>`, published by CI on main).

| Path | What |
|---|---|
| `helm/wtc/` | Helm chart — the primary packaging (in-cluster) |
| `docker-compose.yaml` | VMs / local |
| `wtc.example.yaml` | starter config for compose (`cp` to `wtc.yaml`, edit) |

Two properties are contracts, not defaults to tune:

- **`replicas: 1` + `Recreate` + RWO PVC** — SQLite has a single writer.
  Scaling horizontally corrupts nothing but gains nothing; the second pod
  fails to start (by design).
- **`${VAR}` in config resolves at startup and an unset var is fatal** —
  only reference variables your `existingSecret` actually provides. The
  chart's default config boots secretless (API rejects everything until
  `auth.api_tokens` is configured).

Verified: chart installed into a kind cluster (pod Ready, authed ingest);
compose boot + round-trip.
