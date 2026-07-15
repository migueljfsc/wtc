# Changelog

Notable changes to wtc. Format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); SemVer once releases begin.

## [Unreleased]

### Operability (post-P10)

- **GitHub poller auto-discovery**: leave `sources.github.repos` empty to watch
  every repo the token can access (owner/collaborator/org member; archived
  skipped; the set is re-checked each poll).
- **Helm — deploy the portal by default** (`ui.enabled: true`) + an opt-in
  single-host **ingress** that serves the SPA and proxies `/api` to wtc on the
  same origin (no CORS).
- **Helm — `env`/`secretKeyRef`**: inject secrets from any existing Secret + key,
  as an alternative to `existingSecret` (which requires `WTC_*`-named keys).
- **Diff matrix coloring**: a cell is amber when it is *behind* the newest deploy
  in its row (the laggard to promote), not when it differs from the rightmost
  column — so up-to-date envs are no longer flagged.
- **Onboarding guide** (`docs/setup/onboarding.md`): Helm install + GitHub poller
  + Flux, end to end.

### Added — Phase 10 (live + config surfaces)

- **Live updates (SSE):** the store's single writer publishes each newly-stored
  event to a broadcaster; `GET /api/v1/stream` pushes them as `text/event-stream`
  (bearer-authed; heartbeats). The portal consumes it with `fetch` (EventSource
  can't set the auth header) and coalesce-invalidates its queries, so the
  timeline and dashboard update **without polling** — with a header "Live"
  indicator. Re-ingested duplicates are suppressed so the stream can't flood.
- **Editable config with hot-reload:** `rules` and `tag_patterns` are editable
  from the portal's Settings page. `PUT /api/v1/config/rules` (and
  `/tag_patterns`) validate by compiling, persist to a new `config_overrides` DB
  table, and **atomically swap** the engine/resolver — so a **subsequently-
  ingested event is re-routed with no restart**, and it works even when
  `wtc.yaml` is mounted read-only. `DELETE` reverts to the YAML baseline
  (precedence: DB override > file). Engine/resolver are held behind hot-swap
  holders shared by the webhook handlers **and** the poller.
- **Read-only config + source health:** `GET /api/v1/config` exposes the
  effective rules + tag_patterns (defaults surfaced) with `*_overridden` flags;
  the Settings page also renders `/doctor` as a source-health view.
- Token management and multi-user auth remain out of scope (RBAC non-goal).
- Migration `0004_config_overrides.sql`; OpenAPI + drift + tests
  (broadcaster, SSE end-to-end, `/config`, edit/validate/persist/reset,
  engine-holder hot-swap).

### Added — Phase 9 (change-intelligence views)

- **Where visualized** (`ui/`): search a ref (or arrive via `?ref=`) → per-env
  BUILD→INTENT→APPLIED pipeline with intent→applied lag and dashed gap/unknown
  markers.
- **Diff visualized** (`ui/`): a services × environments matrix with
  promotion-ordered columns (dev→…→prod heuristic), drift highlighted,
  not-yet-promoted flagged, revision-only caveat marked; cells deep-link to
  Where. Backed by a new **`GET /api/v1/matrix?envs=`** endpoint (latest
  succeeded deploy per cell, reusing `LatestSucceededDeploys`; default columns
  exclude ephemeral `pr-*`).
- **Service detail** (`ui/`): current version across every env, deploy
  frequency / change-failure rate / MTBF (computed client-side), recent
  failures, and deploy history. (Lead-time is deferred — it needs a
  build↔deploy join.)
- **Alert correlation** (`ui/`): opening an alert event shows a timeline of the
  changes in the preceding window (selectable 30m–24h, mirroring
  `wtc around --window`), closest-to-alert highlighted.
- OpenAPI + drift test + store test cover the matrix endpoint; routes stay
  code-split.

### Added — Phase 8 (portal core views)

- **Dashboard** (`ui/`): window control (14/30/90d), headline tiles, an activity
  bar chart (events with failures stacked, palette validated for light+dark),
  per-env health cards (deploy count, failure rate, last-deploy status), and a
  recent-changes feed.
- **Timeline** (`ui/`): faceted filter bar (env/service/kind/status/actor +
  full-text search), infinite scroll over cursor pagination, saved filters
  (client-side), and an event-detail drawer showing the full redacted payload
  and the event's inline `where`-journey. Routes are code-split so Recharts
  loads only with the dashboard.
- **Aggregation endpoints** (Go, no new deps): `GET /api/v1/stats/activity`
  (gap-filled day/hour event-count buckets, capped), `GET /api/v1/stats/deploys`
  (per-env deploy count/failures/services/last-deploy health), and
  `GET /api/v1/facets` (distinct env/service/actor for filter dropdowns).
- **`/api/v1/events`** gains an exact `actor=` filter (FTS `q` already searched
  actor text; this is the facet equality filter). Stats windows are inclusive of
  `until` so "just now" events aren't dropped.
- OpenAPI spec extended for all of the above; the drift test and the generated
  typed client cover them.

### Added — Phase 7 (portal foundation)

- **Portal SPA scaffold** (`ui/`): a separate rich client — Vite + React 18 +
  TypeScript + Tailwind + shadcn-style components + TanStack Query + React
  Router. App shell (nav + light/dark theming), token-login screen, and empty
  view stubs for P8–P10 (Dashboard, Timeline, Where, Diff, Services, Settings).
  Its own toolchain; never touches the Go build. Built and deployed as its own
  image.
- **API hardening on the Go server (additive — `/api/*`, `/`, and the CLI are
  unchanged):**
  - **`/api/v1/*`** — every query route now also answers under a versioned
    prefix (same handler, so the two can't drift). `apiRoutes()` is the single
    registration source.
  - **CORS** — `server.cors.allowed_origins` (off by default; `*` allows any).
    Answers preflight `OPTIONS`, echoes the allowed origin with `Vary: Origin`,
    and carries the header on errors so the browser can read them.
  - **OpenAPI** — hand-authored 3.0 spec served at `/api/openapi.json`; the
    portal generates its typed client from it. `TestOpenAPINoDrift` fails if a
    route is added without a spec entry; CI fails if the committed client is
    stale.
  - **Token-login** — `GET /api/v1/auth/verify` lets the SPA validate a bearer
    token with no side effect (200/401).
- **Packaging:** `ui/Dockerfile` (build → nginx) with runtime API-base-URL
  injection (`WTC_API_BASE_URL`, one image any server); CI `ui` job (lint +
  typecheck + build + client-drift check) and gated `ghcr.io/…/wtc-ui` image;
  docker-compose and the Helm chart gain an opt-in `ui` service;
  `docs/setup/portal.md` wires both containers (direct-CORS and same-origin
  proxy paths).

### Added — Phase 6 (release hygiene)

- **Retention prune job** (SPEC §8): `retention:` config (`keep`,
  `ephemeral_env_pattern`, `ephemeral_keep`, `interval`). Opt-in — off until
  `keep` is set, so a fresh box never silently deletes. Ephemeral `pr-*` envs
  get a shorter window; unmapped `env=""` rows follow the normal `keep`. Runs
  on the single writer connection (serializes with ingest), reclaims pages via
  `PRAGMA incremental_vacuum`, and the FTS index stays consistent via the
  existing delete trigger. Prunes once on start then every `interval`.
- **`wtc demo`**: seeds a self-contained synthetic week (builds → dev/staging/
  prod deploys with realistic lag, build failures, ephemeral `pr-*` envs, a
  manual change, an alert) through `/ingest/generic`, so `log`/`where`/`diff`/
  `around` and the UI work with zero real wiring. `--days N` (default 7); each
  run is now-anchored and accumulates.
- **Duration config** now accepts standalone `d`/`w` suffixes (`180d`, `2w`),
  not just `time.ParseDuration`'s `ns…h`.
- **`wtc doctor`** reports the oldest retained event (`oldest_event`) as a
  quick retention gauge.
- **goreleaser** (`.goreleaser.yaml` + `release.yml`): cross-platform binary
  archives (linux/darwin × amd64/arm64, static, no CGO) attached to the GitHub
  Release on each `v*` tag; container image still built separately by CI.
- **LICENSE**: added the Apache-2.0 text the repo had declared but was missing.
- **Load sanity test**: 10k events, `log`/`diff` query medians asserted under
  the 100ms SPEC budget (observed ~0.2ms/15ms).

### Added — Phase 5 (surfaces)

- **Embedded web timeline** at `/` (toolchain-free: hand-written
  HTML/CSS/vanilla JS, `go:embed`, no node/bundler). Filter bar
  (search/env/service/kind/since), day-grouped status-colored stream, source
  deep links, cursor load-more, localStorage token, auto-refresh, mobile
  layout. Served behind the mux catch-all so API routes are never shadowed;
  no external asset fetches.
- **Alertmanager ingest** (`POST /ingest/alertmanager`): normalizer built
  against a real Alertmanager 0.33 v4 webhook. One event per alert episode —
  firing→started, resolved→succeeded on `am:<fingerprint>:<startsAt>` with
  duration = endsAt−startsAt. kind=alert (correlation only).
- **`wtc around <ts|alert-id>`** + `GET /api/around`: what changed in the
  window before an instant or an alert event.
- **Slack digest**: `wtc handoff --slack-webhook <url>` posts the digest as
  Slack mrkdwn; optional serve-side scheduler via a `digest:` config section
  (interval + window + webhook), first post one interval after startup.

### Added — Phase 4 (gap closers + packaging)

- **`wtc wrap -- <command>`**: records started → succeeded/failed on one row
  with duration and exit code; inherited stdio; exit code passthrough; a dead
  server warns and never blocks. Sniffers: helm upgrade/install (release →
  service, namespace, chart, `--set image.tag`), terraform/tofu apply/destroy
  (`-json` change_summary counted into the title — plan bodies never stored).
- Merged revert PRs (`Revert "..."`) land as kind=rollback.
- Flux `Progressing` pre-events dropped (phantom started-forever rows).
- Generic ingest accepts structured `details` (exit codes, change counts)
  into the payload.
- **Packaging**: scratch Dockerfile (CI publishes `ghcr.io/migueljfsc/wtc`),
  Helm chart under `deploy/helm/wtc` (single replica + Recreate + PVC — the
  SQLite contract), `deploy/docker-compose.yaml` for VMs/local.
  Chart verified by installing into a kind cluster; compose boot verified.
- Docs: `docs/setup/{wrap,deploy,gha-report-step}.md`.

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
