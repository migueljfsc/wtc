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

### Fixed — post-phase-0 review (multi-agent code review, 22 confirmed findings)

- **Config**: referencing an unset `${VAR}` is now a load error (previously expanded to `""`; `db: ${UNSET}` silently opened an ephemeral temp database — total data loss on restart). `store.Open("")` also rejected. Empty `server.listen`/`server.db` rejected.
- **Upsert**: strict-outrank guard (equal-rank stale replays can no longer flip `succeeded↔failed`); non-empty-wins merge for payload/duration/url/identity fields (a completion event without artifacts no longer destroys the started event's artifact payload; identity resolved at completion now enriches the row).
- **Shutdown**: `Store.Close` is safe against concurrent `Ingest` (no more send-on-closed-channel panic when HTTP shutdown times out); late ingests get 503.
- **Generic ingest hardening**: `source` restricted to `generic|manual|helm|terraform`; dedup_key prefixes `gh:`/`flux:`/`am:` rejected — a bearer-token client can no longer overwrite rows owned by dedicated ingest paths.
- **Redaction implemented now** (was deferred to Phase 1): `internal/normalize` regex deny-list (AWS keys, GitHub PATs, bearer tokens, password/secret/token k=v) scrubs title+payload before storage on every ingest path.
- **API**: malformed cursor → 400 (was 500); `q=` param → 400 until FTS lands in Phase 3 (was silently ignored, returning unfiltered data).
- **CLI**: `wtc log --json` now emits `{events, next_cursor}` (was a bare array that silently dropped the pagination signal); inverted `--since`/`--until` windows rejected; huge durations rejected instead of overflowing; `wtc record` gained `--duration-ms`/`--artifacts` and honest `--dedup-key` retry docs.
- **Perf/cleanup**: single-statement upsert via `RETURNING` + prepared statements on the write path; pooled ULID entropy (`ulid.Make`); shared `ErrorResponse` type between server and client; `cmp.Or` for fallback chains; dead config fields removed.

### Known gaps (by design, later phases)

- No rules engine, capture mode, retention job, or FTS yet.
