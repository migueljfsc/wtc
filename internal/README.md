# internal/ — the engine

Data flows left to right: sources → normalization → store → queries. Every
package has a doc comment; this is the map.

| Package | Role |
|---|---|
| `model` | The one schema everything maps onto: `Event`, source/kind/status enums, validation, status ranking (upsert rule), canonical timestamp format (fixed-ms RFC3339 — lexicographically sortable as TEXT) |
| `config` | Hand-rolled wtc.yaml loader: `${VAR}` expansion (unset var = fatal, by design), `WTC_*` env overrides, defaults |
| `store` | **Sole owner of the database.** SQLite default (WAL) or opt-in Postgres (P15) behind one query surface (`?`→`$n` rebind + explicit dialect branches); per-dialect embedded migrations, single-writer goroutine over an ingest channel, read pool, the strict-outrank + non-empty-wins dedup upsert, query helpers, doctor stats, poller watermarks, `wtc migrate` ledger copy |
| `normalize` | Cross-source pipeline: ordered rules engine (env/service inference — globs, templates, first-writer-wins), redaction deny-list, `tag_patterns` resolver (tag↔sha, powers `where`) |
| `ingest/github` | REST payload structs + normalizers (workflow_run/PR/commit, built on `testdata/github/rest/`), the **API poller** (primary ingest: watermarks, bounded backfill, 1h overlap), PR-diff enrichment (paths facts + image-bump extraction) |
| `ingest/flux` | notification-controller eventv1 parsing (built on `testdata/flux/`), severity→status, revision→ref extraction, suppression window (reconcile re-emit shedding), Progressing drop |
| `ingest/generic` | `/ingest/generic` + `wtc record`/`wrap` schema; source/dedup-prefix restrictions so bearer clients can't spoof dedicated ingest paths |
| `ingest/mapping` | Config-declared **mapping webhooks** (`/ingest/webhook/<name>`, P14): compiled payload→Event templates + `dedup_key` + rule facts, static-token/HMAC auth, `go:embed` presets (grafana/jenkins). Each name registers as a first-class `model.Source` |
| `server` | HTTP surface: bearer auth (constant-time, fail-closed), GitHub `X-Hub-Signature-256` + Flux `X-Signature` HMAC verification, capture mode (fixture harvesting), all `/api/*` handlers |
| `query` | The composed queries: `where` (BUILD → INTENT → APPLIED per env with lag), `diff` (latest deploy per service/env, revision-only caveats), `handoff` (markdown digest) |
| `client` | Thin HTTP client used by every CLI subcommand except serve — **the CLI never opens the DB file** |
| `wrap` | `wtc wrap`: started→terminal lifecycle around any command, helm/terraform arg sniffers, terraform `-json` change-summary counting, never blocks on a dead server |

Invariants worth knowing while reviewing (details in `CLAUDE.md` "known traps"):

- Ingestion is **at-least-once everywhere**; correctness comes from
  `dedup_key` upserts, not delivery guarantees.
- An update only applies when the incoming status **strictly outranks** the
  stored one; non-empty fields merge, empty ones never blank stored data.
- Path-based rules **skip** (not "no match") when the changed-file list is
  truncated or unknown.
- Redaction runs before storage on every ingest path.
