# Backing up the ledger

wtc is a system of record — treat the database like one. The shape depends on
the backend.

## SQLite (default): `wtc backup`

```bash
wtc backup ./wtc-$(date +%F).db
# backup written: ./wtc-2026-07-19.db (1204224 bytes)
```

The server takes a **consistent point-in-time snapshot with `VACUUM INTO`**
while it keeps serving (WAL-safe; the copy comes out compacted), streams it
over `GET /api/backup`, and the CLI writes it atomically (temp file + rename).
The CLI never touches the DB file — this works against a remote server, and
the snapshot opens like any wtc database:

```bash
wtc serve --config <(echo 'server: {listen: ":9999", db: ./wtc-2026-07-19.db}
auth: {api_tokens: [check]}')   # inspect a snapshot in isolation
```

### Cron → object store

A nightly snapshot shipped off-host is the 90% solution for a change ledger
(retention typically keeps months, and the poller re-ingests recent GitHub
history idempotently after a restore):

```bash
#!/bin/sh -e
# /etc/cron.daily/wtc-backup
f=/tmp/wtc-$(date +%F).db
wtc backup "$f" --server https://wtc.internal.example.com --token "$WTC_API_TOKEN"
aws s3 cp "$f" "s3://acme-backups/wtc/$(basename "$f")"   # or rclone/mc/gsutil
rm -f "$f"
```

In-cluster, the same three lines fit a `CronJob` using the `ghcr.io/…/wtc`
image (the binary is the client; point `--server` at the service).

### Continuous replication: litestream

For point-in-time recovery instead of nightly granularity, run
[litestream](https://litestream.io) as a sidecar next to `wtc serve` —
it tails the WAL to object storage continuously:

```yaml
# litestream.yml
dbs:
  - path: /data/wtc.db
    replicas:
      - type: s3
        bucket: acme-backups
        path: wtc
        region: eu-central-1
```

Restore with `litestream restore -o /data/wtc.db s3://acme-backups/wtc`.
Litestream needs the same filesystem as the DB (sidecar container sharing the
PVC, or the same VM) — it complements, not replaces, `wtc backup` for
off-cluster copies.

## Postgres backend

`wtc backup` answers **501** on postgres — the database is not wtc's to
snapshot. Use the ecosystem:

```bash
pg_dump --format=custom --file=wtc-$(date +%F).dump "$WTC_PG_DSN"
```

or your managed provider's automated backups/PITR (RDS, Cloud SQL, neon —
all cover this better than wtc could). `wtc migrate` remains the path for
moving a sqlite ledger *into* postgres, not a backup tool.

## What a restore looks like

1. Stop `wtc serve` (single writer — never point two at one file).
2. Put the snapshot at `server.db`'s path (or restore the pg dump).
3. Start serve. Pollers resume from their stored watermarks; anything the
   snapshot missed inside the backfill window is re-ingested idempotently
   (stable dedup keys). Webhook-only events lost between snapshot and crash
   are gone — that gap is what litestream shrinks.

## Related

- [export.md](export.md) — take the *data* with you (CSV/NDJSON), as opposed
  to the database file
- [postgres.md](postgres.md) · [retention.md](retention.md)
