# Exporting the ledger (audit / analysis)

`wtc export` streams the filtered change record out of the server — the
"every prod change in Q3" question, answered in one command. Filters mirror
`wtc log`; ordering is newest first; large ranges stream page-by-page and
never buffer.

```bash
# Q2 prod changes, as a spreadsheet
wtc export --env prod \
  --since 2026-04-01T00:00:00Z --until 2026-07-01T00:00:00Z > q2-prod.csv

# full events (payload + facts included), one JSON object per line
wtc export --format ndjson --since 30d --out changes.ndjson

# every failed deploy this quarter, as a JSON array
wtc export --kind deploy --status failed --since 90d --format json
```

Formats:

- **csv** (default) — the flat columns in a **stable, append-only order**
  (`id, ts, ingested_at, source, kind, status, env, cluster, namespace,
  service, repo, actor, ref, artifact, title, url, duration_ms, dedup_key`).
  Safe to build audit scripts against; new columns only ever append.
- **ndjson** — one full event per line, `payload` and `facts` included.
  The right input for `jq`, warehouse loads, or re-ingestion tooling.
- **json** — the same events as a single array, for tools that want one
  document.

The API form is `GET /api/export?env=prod&since=…&format=csv` (bearer-authed
like the rest of `/api/*`), so a compliance job can pull straight from the
server without the CLI.

Ad-hoc analysis pairs well with ndjson:

```bash
# deploys per service, last 30 days
wtc export --kind deploy --since 30d --format ndjson |
  jq -r .service | sort | uniq -c | sort -rn
```

For a copy of the *database* rather than the data, see
[backup.md](backup.md).
