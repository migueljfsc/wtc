# wtc — what the change

A vendor-neutral change ledger: "git log for production".

`wtc` is a single self-hosted binary that ingests change events from heterogeneous sources — CI builds, GitOps reconciles, helm/terraform runs, manual changes — normalizes them into one schema, and answers three questions fast:

1. **What changed?** — `wtc log --env prod --since 2h`
2. **Where is this commit?** — `wtc where <sha>` (build → PR merged → reconciled per env)
3. **How do two environments differ right now?** — `wtc diff staging prod`

## Why

New Relic, Datadog, and Harness all offer change tracking — locked inside their platforms. Nothing neutral, standalone, and open exists. `wtc` is CLI-first, self-hosted, and depends on no single vendor's ecosystem.

## Status

Pre-alpha. Currently in the design/spec phase — see [docs/SPEC.md](docs/SPEC.md) and [docs/PLAN.md](docs/PLAN.md). Nothing runnable yet.

## Design at a glance

- Single static Go binary, no CGO. SQLite (WAL) storage.
- `wtc serve` is the daemon (ingest HTTP + query API); every other subcommand is a thin HTTP client.
- First-class ingest: GitHub (API poller or webhooks) and Flux notification-controller. Anything else via `/ingest/generic`, `wtc record`, or `wtc wrap`.
- Runs happily inside a private network — no public endpoint required (the GitHub poller pulls instead of waiting for webhooks).
- At-least-once ingestion with stable dedup keys — webhook loss is recoverable.
- Secrets redacted before storage.

## License

Apache-2.0
