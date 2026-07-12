# Changelog

Notable changes to wtc. Format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); SemVer once releases begin.

## [Unreleased]

### Added — Phase 0 (skeleton)

- Go module, Makefile, GitHub Actions CI (build + vet + test + lint).
- `wtc` CLI (cobra): `serve`, `record`, `log` working end-to-end; `init`, `doctor` stubs.
- Config loader: single YAML file, `${VAR}` expansion, `WTC_*` env overrides.
- SQLite store (modernc.org/sqlite, no CGO): WAL mode, embedded sequential migrations, single-writer goroutine over an ingest channel, read-only query pool.
- Idempotent ingestion: `dedup_key` UNIQUE upsert with status ranking (`started` never regresses a completed row).
- HTTP server: `GET /healthz`, `POST /ingest/generic` (bearer auth), `GET /api/events` (bearer auth, filters + cursor pagination).

### Known gaps (by design, later phases)

- Payload redaction lands in Phase 1 — avoid posting secrets to `/ingest/generic` until then.
- No rules engine, capture mode, retention job, or FTS yet.
