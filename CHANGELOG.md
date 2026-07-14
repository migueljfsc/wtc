# Changelog

Notable changes to wtc. Format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); SemVer once releases begin.

## [Unreleased]

### Added — Phase 3 (the three killer queries)

- **`wtc where <sha|tag>`** — BUILD → INTENT → APPLIED per environment with
  intent→applied lag. Accepts short/full shas or image tags via the
  configurable `tag_patterns` (defaults: `sha-<sha>`, `<semver>-<sha>`).
  Explicit unknown markers; wtc never guesses.
- **`wtc diff <a> <b>`** — latest successful deploy per service across two
  envs; artifact comparison with flagged revision-only fallback, drift age,
  only-in-one-env detection.
- **`wtc handoff --since 7d`** — markdown digest: deploys/failures per env,
  infra changes, rollbacks, unmapped counts, top actors, first-seen services.
- **Full-text search**: `wtc log -q <text>` / `?q=` over
  title/service/actor/artifact (FTS5, prefix matching, injection-safe).
- **PR-diff enrichment**: merged PRs gain changed-file facts (path rules can
  now infer env for promotion PRs) and `image_bumps` payload extracted from
  kustomize/helm patches — the tag↔manifest-revision link `where` traverses.
- `demo/`: three dummy services with commitizen lifecycles, GHCR pipelines,
  kustomize overlays, and Flux wiring posing as three clusters — the living
  test bed generating real events.

### Added — Phase 2 (Flux ingest)

- `POST /ingest/flux`: generic-hmac verification (`X-Signature: sha256=<hex>`,
  constant-time, fail-closed; format pinned by real captured deliveries).
- Flux normalizer built against fixtures captured from a live kind cluster
  running Flux v2.9 (`testdata/flux/`): Kustomization reconcile success +
  failure, HelmRelease install. severity → status; `master@sha1:<sha>`
  revisions extracted into `ref` (the `wtc where` join); chart versions land
  in `artifact`; Alert `eventMetadata.cluster` → cluster field → env via rules.
- Suppression window (trap #1): re-emits of the same (object, revision,
  reason) are shed in-memory before the write path; the strict-rank dedup
  upsert remains the correctness backstop. Live-verified: 6 notification
  deliveries → 1 row.
- `sources.flux` config (hmac_key, suppression_window), rules applied on the
  webhook path, docs: `docs/setup/flux.md` + `flux-provider.yaml`.
- Deferred pending capture on a real cluster: ImageUpdateAutomation events
  (git side of image automation already covered by GitHub push ingest).

### Added — Phase 1 (GitHub ingest, poller-primary)

- **GitHub API poller** — primary ingest for private deployments (no public
  endpoint needed): workflow runs, merged PRs, default-branch commits per
  configured repo; per-(repo,resource) monotonic watermarks (migration 0002);
  bounded 24h first-run backfill; 1h overlap re-reads so in-progress runs get
  their terminal status; idempotent by dedup key, doubling as the
  webhook-loss sweeper.
- **Normalizers** built against real captured fixtures (`testdata/github/rest/`,
  8 payloads incl. a full queued→in_progress→completed lifecycle):
  `workflow_run` → build (status/conclusion mapping, duration, run_attempt in
  dedup key), merged PR → merge, commit → push. Parsers never guess env.
- **Rules engine** (SPEC §3): ordered rules, `*`/`**` globs, first-writer-wins
  per field, template funcs (`trimPrefix`/`trimSuffix`/`lower`/`regexReplace`),
  truncated path lists skip path rules instead of mis-routing.
- `POST /ingest/github`: `X-Hub-Signature-256` verification (constant-time,
  fail-closed) + delivery capture; envelope normalization deferred until
  webhook fixtures exist.
- **Capture mode** (`--capture-dir` / `server.capture_dir`): dumps raw ingest
  bodies + headers per source for fixture freezing.
- `wtc doctor` + `GET /api/doctor`: per-source last-event age and 24h counts,
  unmapped-env count with samples, clock-skew flags, db size, poller
  watermarks; `--max-silence` exits non-zero for silent sources.
- `wtc init`: scaffolds a commented wtc.yaml and prints the wiring checklist.
- `sources.github` config (token, poll interval, repos, infra_path, webhook
  secret), `rules:` section, YAML duration type.
- Docs: `docs/setup/github-poller.md`, `docs/setup/github-webhook.md`.

Live-verified against migueljfsc/{wtc,portfolio,aws-app-platform,
motorcycle-journey}: events visible within one poll interval; a second sweep
adds zero duplicate rows.

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
