# Postgres backend (stateless wtc pod)

SQLite is the default and stays the single-binary story. Opt into Postgres
when you want the wtc pod **stateless**: no PVC, RollingUpdate upgrades,
instant reschedule on node loss, and your standard database backup/HA story
(RDS, CloudNativePG, …). The driver is operational posture, not scale —
SQLite handles wtc's volumes for years.

Still **one replica**: pollers, suppression windows, and the digest are
per-pod. Idempotent ingest makes an accidental second replica safe but
wasteful — it is not a supported topology.

## Config

```yaml
storage:
  backend: postgres          # default: sqlite (server.db stays the file path)
  dsn: ${WTC_STORAGE_DSN}    # carries credentials — inject via env/secret
```

Or pure env (no config change): `WTC_STORAGE_BACKEND=postgres` +
`WTC_STORAGE_DSN=postgres://user:pass@host:5432/wtc?sslmode=...`.
`backend: postgres` without a DSN is a startup error — fail fast, never a
silently-wrong ledger. Migrations run automatically at startup (the postgres
schema has its own embedded sequence).

## Helm

Secrets follow the chart's single-`existingSecret` contract: one
operator-managed Secret with **opinionated keys** carries API tokens, source
credentials, and DB auth together (see `values.yaml` for the full key list).
A quick chart-managed path exists for the bundled postgres password.

**Bundled postgres pod** (single-node StatefulSet). The chart renders wtc's
DSN into the ConfigMap referencing `${WTC_PG_PASSWORD}`; wtc's own config
loader expands it at startup (unset = fatal), so the password never appears in
the Deployment spec or ConfigMap. Password, one of:

```bash
# operator-managed (recommended): one Secret for everything
kubectl -n wtc-system create secret generic wtc-secrets \
  --from-literal=WTC_API_TOKEN=<random> \
  --from-literal=WTC_PG_PASSWORD=<random, URL-safe>
```

```yaml
storage: { backend: postgres }
postgresql: { enabled: true }
existingSecret: wtc-secrets
```

or the quick path — `postgresql.auth.password: <value>` and the chart manages
the Secret itself. Either way the wtc PVC is dropped and the Deployment
switches to RollingUpdate.

**Bring your own database** (managed/HA — recommended for production). Point
at CloudNativePG, RDS, or any postgres ≥ 14. Two shapes:

```yaml
# a) URL in values, password from the secret (config ${VAR} expansion):
storage:
  backend: postgres
  externalDatabase:
    url: postgres://wtc:${WTC_DB_PASSWORD}@your-db:5432/wtc
existingSecret: wtc-secrets    # provides WTC_DB_PASSWORD

# b) whole DSN out-of-band: leave url EMPTY and put the full DSN in the
#    secret as WTC_STORAGE_DSN — it reaches wtc as an env override.
storage:
  backend: postgres
  externalDatabase: { url: "" }
existingSecret: wtc-secrets    # provides WTC_STORAGE_DSN

## docker-compose

```bash
cd deploy/compose
WTC_PG_PASSWORD=change-me docker compose \
  -f docker-compose.yaml -f docker-compose.postgres.yaml up -d
```

The overlay adds a `postgres` service and flips wtc to it via `WTC_STORAGE_*`
env — `wtc.yaml` needs no storage section.

## Migrating an existing sqlite ledger

Poller backfill is bounded (≈24h), so switching backends without a copy would
lose history. One-shot, offline, idempotent:

```bash
# 1. stop `wtc serve`
wtc migrate --from ./wtc.db --to 'postgres://wtc:...@host:5432/wtc'
# 2. set storage.backend=postgres (+ dsn), start serve
```

Copies events, poller watermarks, and DB-backed config overrides; re-running
skips existing rows (`ON CONFLICT DO NOTHING`). With `--config` it defaults
`--from`/`--to` from `server.db`/`storage.dsn`. This is the one deliberate
exception to "the CLI never opens the DB file" — an offline admin operation on
a stopped ledger.

## Behavioral differences vs sqlite

- **Search** (`wtc log -q`, portal search): sqlite uses an FTS5 index
  (word-prefix matching); postgres uses per-term case-insensitive substring
  match (`ILIKE`) — deliberately unindexed, the events table is small. Add
  `pg_trgm` yourself only if search ever measurably hurts.
- **Retention**: `retention.ephemeral_env_pattern` supports `*` and `?` on
  both backends; sqlite-GLOB character classes (`[...]`) are sqlite-only.
  Space reclamation is `incremental_vacuum` on sqlite, autovacuum on postgres.
- Everything else — dedup/upsert semantics, queries, API, CLI output — is
  identical; the parity test suite (`TestPG*`, CI postgres service) holds the
  two backends to the same answers.
