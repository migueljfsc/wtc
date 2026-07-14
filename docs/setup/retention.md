# Retention

Prune old events so the ledger (and the SQLite file) stay bounded. The whole
job is **opt-in**: it does nothing until you set `keep`, so a fresh install
never silently deletes.

Add to `wtc.yaml`:

```yaml
retention:
  keep: 180d                     # normal envs (and unmapped env="") older than this go
  ephemeral_env_pattern: "pr-*"  # SQLite GLOB; default "pr-*"
  ephemeral_keep: 30d            # shorter window for ephemeral envs; defaults to keep
  interval: 24h                  # run cadence; default 24h once keep is set
```

Durations accept `s`/`m`/`h` plus standalone `d`/`w` (`180d`, `2w`).

## Behaviour

- Two windows: rows whose `env` matches `ephemeral_env_pattern` are pruned at
  `ephemeral_keep`; everything else — including unmapped `env=""` rows — at
  `keep`. An ephemeral row is never kept longer than `ephemeral_keep` just
  because `keep` is larger.
- Runs once on startup (cleans stale rows even on a box that restarts daily),
  then every `interval`.
- Deletes on the single writer connection, so it serializes with ingest — no
  `SQLITE_BUSY`. Freed pages are reclaimed with `PRAGMA incremental_vacuum`
  (the DB is opened `auto_vacuum=INCREMENTAL`). The full-text index stays in
  sync automatically.
- Disabled entirely when `keep` is unset or zero.

## Check it

`wtc doctor` reports total events, DB size, and the oldest retained event —
eyeball `oldest_event` against `keep` to confirm the prune is keeping up:

```
events: 12043 total · db 4.2 MiB · oldest 179h ago
```
