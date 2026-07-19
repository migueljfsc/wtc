# Changelog

Notable changes to wtc. Format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); SemVer once releases begin.

## [Unreleased]

### Added — harden the record: `wtc export` / `wtc backup` / `wtc explain` (P22)

- **`wtc export`** — stream the filtered ledger for audit/analysis ("every
  prod change in Q3"): `--env/--service/--repo/--kind/--status/--source/
  --since/--until`, formats **csv** (flat columns, stable append-only order),
  **ndjson** (full events incl. payload + facts) and **json** (array).
  Streaming `GET /api/export` pages internally — large ranges never buffer.
- **`wtc backup <path>`** — consistent point-in-time snapshot of the sqlite
  ledger, taken with `VACUUM INTO` while serving (WAL-safe, compacted),
  streamed over `GET /api/backup` and written atomically client-side; works
  against a remote server (the CLI still never opens the DB). Postgres
  answers 501 — use `pg_dump` (documented). New `docs/setup/backup.md`
  (cron → object store, litestream sidecar, restore semantics) and
  `docs/setup/export.md`.
- **`wtc explain <event-id>`** — which rule set each of
  env/cluster/namespace/service/kind/actor: a first-writer-wins trace (rule
  index + match spec, "normalizer", or "unmatched") from replaying the
  **current** rules over the event's recorded ingest-time facts; divergence
  from the stored row (rules edited since ingest) is flagged, never hidden.
- **New `facts` column** (sqlite `0006` / postgres `0005`, nullable):
  `Engine.Apply` now records the redacted rule facts plus the pre-rules field
  snapshot on every engine-ingested event — facts are not reconstructible
  from stored payloads, so this is what makes explain honest. Rows ingested
  before the migration (or via generic/record/wrap, which never run the
  engine) report **"facts not recorded"** — no guessing. `wtc migrate`
  carries the column to postgres.

### Added — `wtc blast`: incident correlation (P20)

- **`wtc blast <alert-id|ts>`** ranks the changes in the window before an
  alert as suspects — "what changed before this broke?". The score is a
  fixed, documented, deterministic heuristic (never ML): recency 0–30 within
  the window, same env +30 (the hard signal), same service +20 (booster —
  alerts often lack a clean service, so it ranks, never filters), kind
  weight (+15 deploy/rollback/config_change … +2 build), +10 for a
  failed/degraded change right before the anchor. Each suspect carries a
  human-readable `why` breakdown.
- **Direction flips on the anchor**: `wtc blast <deploy-id>` lists the alerts
  that fired in the window *after* a change — "did my deploy cause noise?".
- Anchors: an event id or a bare RFC3339 instant; `--env`/`--service` set the
  scoring context for bare instants (without them the same-env signal is
  disabled — reported, never silently guessed). `--window` (default 2h),
  `--limit`, `--json`.
- New `GET /api/v1/blast` (in the OpenAPI contract); `/around` is unchanged.
- **Portal:** the alert drawer's panel is now **Likely causes** — the blast
  ranking with score chips and reasons on hover; suspects with a ref link
  into Where.
- Pure query layer: no schema change, no new dependencies.

### Added — `repo` dimension & timeline facet (monorepo support)

- **Events now carry a `repo` (owner/name)** — persisted and facetable. Filter
  the timeline (and `wtc log --repo`) by source repo, and see it on the event
  detail. Populated on source/CI rows (github/gitlab PR/push/build); stays empty
  for cluster-side events (flux/argo carry no source repo in their payloads).
- **Why:** in a monorepo, a source PR fans out to 0..N deploy units, so the
  single-valued `service` has no honest value for a cross-app change and reads
  as "serviceless". `repo` is the codebase lens (`service`/`env` stay the deploy
  lens): a cross-app PR is now findable by `repo` instead of silently blank.
  `service` still resolves per app on build/deploy rows and single-app PRs.
- `repo` is a raw source-side fact — the rules engine persists it verbatim
  (never inferred); a normalizer that already set it wins.
- Migration `0005`/`0004` (sqlite/postgres): `events.repo` + `idx_events_repo_ts`.
  New events only — existing rows keep `repo=''` until re-ingested by a poller.
- `wtc demo` seeds a monorepo (`acme/storefront`: apps `catalog`/`checkout`
  + a cross-app PR with no single service) alongside the single-service repos.
- Facet grouping (services nested under their repo) is a deliberate fast-follow.

### Added — multi-select timeline facets (incl. new source facet)

- **Every timeline facet is now multi-select** — source, env, service, kind,
  status, actor (OR within a facet, AND across facets). A new compact
  `MultiSelect` dropdown replaces the single-select dropdowns — searchable for
  high-cardinality facets (service/actor), no popover dependency, closes on
  outside-click/Escape.
- New **`source`** facet — filter by `github`/`gitlab`/`flux`/`argocd`/… or any
  mapping-webhook name; dynamic from the ledger (`/facets.sources`, so custom
  webhook sources appear).
- Backend: each `/events?<facet>=` takes a comma-separated OR-set
  (`<col> IN (…)`); `store.Filter` facet fields are now slices.
- Filter dropdowns size to their content (capped), and Clear/Save moved to the
  Timeline page header so selecting a facet no longer shifts the filter row.

### Added — Flux/ArgoCD ingest scope (allow/deny)

- **`sources.flux.scope` / `sources.argocd.scope`** — an ingest-time allow/deny
  list that keeps third-party reconciles/apps (cert-manager, external-dns,
  operator CRDs, …) out of the ledger, the push sources' analog of the
  GitHub/GitLab poller repo scope. Matches on **raw facts** — `namespace`,
  `object_name` (Flux involvedObject.name / Argo app), `object_kind`, `cluster`
  (Flux), `project` (Argo) — never inferred env/service, so the drop decision
  is deterministic. Globs use the shared `*`/`**` dialect.
- Semantics: **deny wins** over allow; empty `allow` ⇒ allow all; empty `deny`
  ⇒ deny none; fields within an entry are AND, entries are OR. Non-matching
  events are dropped before rules/storage (`202 {"status":"filtered"}`) and
  counted by `wtc_filtered_total{source}`. Bad patterns or all-empty entries
  fail `wtc serve` at config load. Surfaced in `wtc config` and the
  Configuration tab. Labels are intentionally out of scope — Flux events carry
  none and Argo's template exposes only `envLabel`.

### Changed — portal layout: service list, page width, filter comboboxes

- **Services tab** now uses a searchable master-detail layout (filter box +
  scrollable list on the left, detail on the right) instead of a wrapping wall
  of pills — scales to many services. Long names truncate with a tooltip.
- **Wider pages** — data-dense tabs (Dashboard, Timeline, Diff, Services,
  Configuration) widen to `max-w-7xl`; form tabs (Where, Settings) to
  `max-w-5xl`. Removes the large left/right gutters on wide screens.
- **Timeline filter comboboxes** are capped (`max-w-[11rem]`) and ellipsize
  overflowing option text, so a long service name no longer stretches the
  `service` select and reflows `actor` onto the next row.
- **Event drawer `ref`** is now a link to the Where page for that ref
  (`/where?ref=…`), closing the drawer on navigation.

### Fixed — Where dead-end on non-git-traceable diff cells

- Diff-matrix cells sourced from OCI artifacts (Flux `OCIRepository`, e.g.
  Crossplane composition modules) or Helm chart versions carry no git sha:
  their revision is a content digest (`name@sha256:…`) or a chart version, not
  a commit. The matrix previously linked every cell to **Where**, so clicking
  one dead-ended on a 400. Cells now link only when git-traceable (a git-sha
  `ref`, or an image tag embedding one via `tag_patterns`); the rest render as
  plain text with an explanatory tooltip.
- **`/api/where`** no longer 400s on an unresolvable ref. An OCI content
  digest, chart version, or typo now returns an empty journey with a note
  ("nothing to trace") — the CLI and portal explain instead of erroring; a
  remaining error is treated as an internal fault (500).

### Added — first-run backfill window + multi-cluster hub docs

- **`sources.github.backfill` / `sources.gitlab.backfill`** — the first-poll
  history window (default `24h`, unchanged behavior). Set `7d`/`30d`/etc. to
  seed a fresh install with more history; it affects only the first sweep of
  each repo/project (the watermark takes over after), at the cost of
  proportional pagination + PR-diff enrichment up front. Surfaced in the
  Configuration tab source cards and `wtc config`; negative values rejected at
  config load.
- **`docs/setup/multi-cluster.md`** — how to run one central wtc ("the hub")
  that ingests Flux/Argo events from N clusters: per-spoke Provider/Alert
  pointing at the hub, cluster identity via `eventMetadata.cluster`, and
  `cluster → env` rules. SCM pollers run once on the hub. No new code — the
  HTTP ingest + cluster-label design already supported it; this documents the
  flagship topology.

### Added — Phase 18 (Poller scope globs + Where links)

- **Glob patterns in poller scope**: `sources.github.repos` /
  `sources.gitlab.projects` entries may be globs (`my-org/*`,
  `my-org/prefix-*`; `*` = one segment, `**` = any depth — the same dialect
  as `rules:` matches, via the newly exported `normalize.CompileGlob`).
  Resolved against discovery **every sweep**, so new repos matching a pattern
  are polled without a restart; exact entries and the empty-list
  "everything accessible" github mode are unchanged. GitHub accepts any glob
  (discovery is affiliation-bounded, a pattern only filters); GitLab requires
  a static namespace prefix — the prefix names the group/user whose projects
  are listed (new `ListNamespaceProjects`, group→user endpoint fallback,
  fixture-captured from gitlab.com), and unscoped patterns fail at startup.
- **Where page links out** (P18-B): build/intent/applied stage cards — and
  the timeline drawer's applied rows — link to `event.url` (the Actions run,
  commit/PR, or deploy) in a new tab when the event carries one; URL-less
  events render exactly as before, never a dead link.

### Added — Phase 17 (Configuration visibility)

- **`/api/v1/config` now carries the full effective config** (post `${VAR}`
  expansion + `WTC_*` overrides): server/storage/auth/sources (github, gitlab,
  flux, argocd, mapping webhooks)/digest/retention/metrics, alongside the
  live-editable rules/tag_patterns. **Secrets never leave the server**: every
  configured secret renders as the constant `"********"` (length-independent,
  no partials), `api_tokens` as a list of masks, and the postgres DSN as
  host/port/database with credentials stripped by the real pgx parser. The
  view is an allowlist DTO built field-by-field — a forgotten config field
  fails safe (not exposed) — and a sentinel guard test proves no secret value
  survives into the JSON. Mapping-webhook templates are shown in full,
  preset-resolved (config-as-code, same exposure class as rules).
- **Portal: Settings → Configuration tab.** Per-source cards (ingest-mode
  badge, parameters, masked secrets, last-event health chip joined from
  doctor), storage & server (with a capture-mode warning), retention & digest
  jobs, and the existing normalization editors unchanged. `/settings`
  redirects to `/configuration`.
- **`wtc config` CLI** — the same endpoint rendered for the terminal
  (`--json` supported): source table with ingest modes, storage/auth/metrics
  summary, jobs. Retention/digest show the scheduler's *effective* defaults
  (interval 24h, pattern `pr-*`, ephemeral keep = keep) and whole-day
  durations render as `180d`, matching the config syntax.
- **Portal: a real Settings tab** (the old one became Configuration): API
  version via the new bearer-authed `GET /api/v1/version` (versions
  fingerprint deployments — not public), UI version injected at build time
  from `package.json`, connection info (endpoint + reachability), theme
  choice (light/system/dark), and session/local-data controls (log out,
  clear saved timeline filters).

### Added — Phase 16 (Prometheus metrics)

- **`/metrics` endpoint** (`prometheus/client_golang`) on the serve process,
  **bearer-authed with `auth.api_tokens`** — wtc may be public (P13 posture) and
  the endpoint leaks source names and activity levels. Instruments:
  `wtc_ingested_total` / `wtc_deduped_total` / `wtc_suppressed_total` /
  `wtc_mapping_errors_total` (all by `source`), `wtc_poll_last_success_timestamp_seconds`
  (by `source`/`repo`/`resource`), `wtc_db_size_bytes` (per-backend, sampled at
  scrape), `wtc_http_request_duration_seconds` histogram (by route `path`,
  `method`, `status`), and `wtc_sse_connections`. Standard `go_*`/`process_*`
  collectors included.
- **Ingest counters live in the single-writer path**, so `ingested`/`deduped`
  stay complete across every source (webhooks, pollers, generic) with no
  per-handler wiring. The HTTP histogram's `path` label is the matched **route
  pattern** (`/api/v1/where/{ref}`), never the raw URL — raw paths carry
  shas/ULIDs and would explode cardinality.
- **Optional separate unauthenticated listener** (`metrics.listen: ":9091"` /
  `WTC_METRICS_LISTEN`) serving only `/metrics`, for in-cluster scrapes where an
  api_token (which also grants `/api/*`) would be over-privileged. Off by
  default; a configured listener that cannot bind is fatal.
- **Helm: ServiceMonitor + scrape-annotation toggle** (`metrics.*`). Two scrape
  models — main port with bearer auth (ServiceMonitor pulls `WTC_API_TOKEN` from
  `existingSecret`; required in this model) or the unauthenticated
  `metrics.port` listener (chart adds a `metrics` container/Service port). The
  ServiceMonitor selector matches only the API Service, never the portal.
- **Docs:** `docs/setup/metrics.md` — scrape configs for both models and example
  alerts (source silent, mapping errors, no-ingest).

### Added — Phase 15 (Postgres backend — stateless wtc pod)

- **Opt-in Postgres storage backend** (`storage.backend: postgres` +
  `storage.dsn`; `WTC_STORAGE_*` env overrides). SQLite stays the default and
  the single-binary story — the driver is operational posture, not scale: on
  k8s the wtc pod goes **stateless** (no PVC, RollingUpdate, instant
  reschedule), and backup/HA becomes your standard database story.
- **One query surface, two dialects.** All SQL stays in sqlite form; a
  transparent `?`→`$n` rebind wrapper covers postgres, and only the genuinely
  divergent sites branch: DB size (`pg_database_size`), clock-skew/churn
  (`EXTRACT(EPOCH)` instead of `julianday`), search (per-term `ILIKE` — no FTS
  index on postgres, deliberately), retention glob (`~` regex translation,
  autovacuum instead of `incremental_vacuum`). Stats bucketing was unified on
  `substr` over the fixed-width ts text — one portable query, no branch. The
  upsert's stored-row columns are now qualified (`events.<col>`): postgres
  rejects unqualified `DO UPDATE` references as ambiguous; sqlite accepts the
  qualified form, so one statement serves both.
- **Per-dialect embedded migrations** (`migrations/sqlite/` unchanged;
  `migrations/postgres/` fresh — no FTS, `duration_ms BIGINT`), same
  append-only rule.
- **`wtc migrate`** — one-shot offline sqlite→postgres ledger copy (events,
  poller watermarks, config overrides; `ON CONFLICT DO NOTHING`, idempotent
  re-run). The deliberate exception to "the CLI never opens the DB file".
  Verified: `wtc log` output byte-identical across the migration.
- **Helm:** `storage.backend=postgres` drops the wtc PVC and switches to
  RollingUpdate; `postgresql.enabled=true` bundles a single-node postgres
  StatefulSet (with an init wait so first boot doesn't race the DB);
  otherwise `storage.externalDatabase.url` points at your own
  (CloudNativePG/RDS). **Secrets follow one contract:** the chart-wide
  `existingSecret` is a single operator-managed Secret with opinionated keys
  covering API tokens, source credentials, AND db auth (`WTC_PG_PASSWORD`
  bundled / `WTC_STORAGE_DSN` external); the DSN is rendered into the
  ConfigMap referencing `${WTC_PG_PASSWORD}` and expanded by wtc's own config
  loader at startup — credentials never appear in the Deployment spec. A
  quick chart-managed path (`postgresql.auth.password`) remains. Verified
  live on kind in both modes: no PVC, pod deleted → ledger intact, rolling
  upgrade, one out-of-band Secret driving both DB auth and API auth.
- **docker-compose:** `docker-compose.postgres.yaml` overlay (postgres service
  + `WTC_STORAGE_*` env; wtc.yaml needs no storage section).
- **Parity suite:** `TestPG*` (gated on `WTC_TEST_PG_DSN`) re-exercises
  upsert lifecycle, search, doctor, retention, watermarks, overrides, stats,
  matrix, and the ledger migration against a real postgres 16; CI gains a
  postgres service container. Still **one replica** — HA/leader-election out
  of scope. New dependency: `jackc/pgx/v5` (pure Go; operator-approved).
- **Docs:** `docs/setup/postgres.md` (config, both Helm modes, compose
  overlay, ledger migration, behavioral differences).

### Added — Phase 14 (Mapping webhook — long-tail ingest)

- **`/ingest/webhook/<name>`: any tool that POSTs JSON becomes config, not
  code.** Operators declare sources under `sources.webhooks[]` — auth, a
  payload→Event field mapping, a stable `dedup_key`, and optional facts. Mapped
  events enter the standard pipeline (rules → redaction → status-rank upsert),
  so lifecycle transitions and env/service inference work as for any source.
  Each webhook `name` is registered as a **first-class source**, so it appears
  under its real name in `wtc log --source <name>`, facets and doctor.
- **Templates reuse the rules engine.** Field/dedup/facts values are Go
  `text/template` over the parsed JSON body, with the *same* funcs `rules[].set`
  uses (`trimPrefix`, `trimSuffix`, `lower`, `regexReplace`) plus `default`.
  Compiled at startup — a bad template or missing `dedup_key`/`kind`/`title`
  fails the daemon, never a delivery.
- **Auth: static token XOR HMAC.** A shared secret in a configurable header
  (constant-time; default `X-WTC-Token`, like argocd/gitlab) or a sender-signed
  hex HMAC (`sha256`/`sha512`/`sha1`, configurable header + stripped prefix,
  like github/flux). Exactly one is required; neither fails closed.
- **Shipped presets** `grafana` and `jenkins` — a preset supplies the template
  surface; the operator supplies name + secret and may override any field.
  Fixtures captured live: Grafana 11.3 via a test contact point; Jenkins
  Notification Plugin serialized by the plugin's own classes (the plugin's
  SSRF guard blocks a private-IP POST, so the body was produced through
  `buildJobState`). Jenkins git-SCM sub-fields (`scm.branch`/`commit`) follow
  the plugin's documented `ScmState` schema — the build-lifecycle fields are
  live-captured. Grafana firing is live; the resolved variant is a minimal edit
  of it.
- **doctor guardrails for the `dedup_key` footgun.** A churn heuristic flags
  rows that share title/kind/status and landed seconds apart under *distinct*
  dedup keys (a key that should have collapsed but didn't); a per-source
  counter surfaces mapping-template eval failures. Template errors reject the
  delivery `422` (retryable) — never a silent drop.
- **`/ingest/generic` stays separate** — the "you own the sender" path needs no
  mapping. Harbor and Terraform Cloud presets are deferred; the doc shows the
  capture-first authoring loop for wiring any novel tool.
- **Docs:** `docs/setup/mapping-webhook.md` (preset wiring + capture-first
  authoring + the dedup_key/doctor guardrails).

### Added — Phase 13 (GitHub webhook completion)

- **`/ingest/github` graduates from capture-only to full ingest.** The
  P1-deferred webhook-envelope parsing lands: `workflow_run`, `push`, and
  `pull_request` deliveries normalize into the same Events and dedup keys the
  poller produces, so webhook and poller are now **peer modes** — the webhook
  for latency, the poller as the idempotent loss-recovery sweeper. Both can run
  together with zero duplicates.
- **Envelope reuse:** the nested `workflow_run`/`pull_request` objects are
  field-identical to the poller's REST structs, so the envelopes reuse them and
  call the *same* normalizers; only `push` needed a shared `pushEvent` builder
  (its commit shape differs), extracted from `NormalizeCommit`. A push fans out
  to one event per commit; only merged PRs land; non-merge PR actions and
  `ping` are acknowledged and dropped (202).
- **Fixtures** captured from real deliveries on `migueljfsc/wtc` via the
  **hook-deliveries API** (bodies recorded even when the target 404s — no
  tunnel): `workflow_run` completed success + failure, `push`, `pull_request`.
  `X-Hub-Signature` is derived from the secret (safe to keep in a fixture),
  unlike a raw token.
- **Docs:** `github-webhook.md` becomes a full wiring guide; onboarding gains an
  **ingest-posture** guide (private → poller-primary; public → webhook + poller
  sweeper). The github poller now captures via `internal/capture` (no `server`
  import), matching gitlab.

### Added — Phase 12 (GitLab ingest)

- **GitLab as the SCM/CI-axis neutrality peer of GitHub** (mirroring Flux↔Argo
  for GitOps). Both ingest modes converge on the same Events and dedup keys, so
  the poller doubles as the webhook-loss sweeper exactly like GitHub.
- **API poller** (`sources.gitlab`, primary for private deploys) — per-project
  watermarks over pipelines, merged MRs, and default-branch commits; bounded
  24h first-run backfill with a 1h re-read overlap. Pipeline list items are
  sparse, so each in-window pipeline gets a detail fetch for
  finished_at/duration/actor. `base_url` targets self-managed instances (empty
  = gitlab.com); `PRIVATE-TOKEN` auth.
- **`POST /ingest/gitlab`** — the peer webhook mode covering Pipeline / Push /
  Merge Request hooks. GitLab does not HMAC-sign bodies (it sends the secret
  verbatim), so auth is a constant-time compare of `X-Gitlab-Token`
  (`sources.gitlab.webhook_secret`) — the same shape as `/ingest/argocd`. A
  push hook fans out to one event per commit; a non-merge MR action is
  acknowledged and dropped.
- **Dedup keys** (SPEC §1): `gl:pipeline:<project>:<id>` (the pipeline id is
  stable across queued→running→completed — one row upserted through the
  lifecycle, trap #5; a *retried* pipeline gets a fresh id and is a truthful
  second row), `gl:mr:<project>:<iid>:merged`, `gl:push:<project>:<sha>`. The
  project `path_with_namespace` plays GitHub's `owner/repo` role.
- **MR-diff enrichment** (SPEC §7 analog) via the MR changes API — real changed
  paths (env inference for promotion MRs) + kustomize/yaml image-tag bumps
  (the tag↔revision link `wtc where` traverses). Reuses the GitHub bump
  patterns; only matched lines stored, never diff bodies.
- **Facts/rules parity** — `source: gitlab` with repo/branch/event/paths/actor,
  so the same path-glob env rules that route GitHub route GitLab. Revert MRs
  land as `kind=rollback`.
- Verified end-to-end against a real gitlab.com project: `wtc where
  sha-<sha>` spans GitLab pipeline (BUILD) → MR merge (INTENT, from the
  enriched bump payload) → Argo CD sync (APPLIED). `docs/setup/gitlab.md` wires
  a real project using only the docs. Capture helper extracted to
  `internal/capture` so ingest packages capture without importing `server`.

### Added — Phase 11 (ArgoCD ingest)

- **`POST /ingest/argocd`** — second GitOps engine alongside Flux (the
  vendor-neutrality proof). Argo CD has no fixed webhook schema, so wtc ships
  the contract: `docs/setup/argocd-notifications.yaml` templates the canonical
  body (verified against Argo CD v3.4.5 with captured fixtures, incl. four
  empirically-found template gotchas documented inline). Auth is a static
  `X-WTC-Token` shared secret compared constant-time (`sources.argocd.
  webhook_secret`) — Argo's templates cannot HMAC-sign bodies.
- **Lifecycle + spam control:** one row per sync *operation*
  (`app`+`revision`+`startedAt`) — Running → Succeeded/Error upserts in
  place, while a retry of the same revision is a new row so the ledger shows
  both attempts (an (app,revision)-only key froze failed rows through later
  successful retries — found live); `Error` (sync never applied) and `Failed`
  both map to failed. Resync re-notifications are shed by
  `sources.argocd.suppression_window`, with the dedup upsert as the
  correctness backstop.
- **New `degraded` status** (outranks succeeded/failed in the upsert):
  `on-health-degraded` upserts the deploying operation's row instead of
  creating an alert row; surfaced across CLI, embedded timeline, and portal.
- **Env inference for Argo** (never cluster=env — destServer is a URL):
  ordered tiers `env` app label > destination namespace > app-name suffix,
  shipped as example rules; the rules engine gains `object_name`/`namespace`
  matchers and `Project`/`DestServer`/`SourcePath`/`EnvLabel` facts.
- **`wtc where` spans engines:** an Argo sync revision feeds APPLIED exactly
  like a Flux reconcile revision — one journey across Flux- and Argo-managed
  envs. Wiring guide: `docs/setup/argocd.md`.

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
