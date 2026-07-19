# wtc — Build Plan

Lives at `docs/PLAN.md`. Each phase ≈ 1–3 Claude Code sessions. A phase is done only when its acceptance criteria pass and the operator can wire it to real infrastructure using only `docs/setup/`.

## Status

| Phase | State | Notes |
|---|---|---|
| P0 skeleton | ✅ 2026-07-13 | + multi-agent review pass: 22 confirmed findings fixed same day |
| P1 GitHub ingest | ✅ 2026-07-13 | poller-primary (Q3 re-order); webhook envelope parsing deferred with fixtures |
| P2 Flux ingest | ✅ 2026-07-13 | captured from a local kind cluster (Flux v2.9); ImageUpdateAutomation fixtures deferred |
| P3 killer queries | ✅ 2026-07-14 | live-validated on the demo stack: real PR promotion traced end-to-end, 37s lag |
| P4 gap closers | ✅ 2026-07-14 | wrap verified against a live helm install; chart+compose verified; image on GHCR |
| P5 surfaces | ✅ 2026-07-14 | embedded timeline UI, Alertmanager + `wtc around`, Slack digest — all live-verified |
| P6 release hygiene | ✅ done | auto cz-versioning, multi-arch images, goreleaser binaries, retention job, `wtc demo` seed, load sanity, LICENSE |
| **P7 portal foundation** | ✅ 2026-07-15 | `ui/` SPA scaffold (Vite/React/TS/Tailwind/shadcn/TanStack Query) + `/api/v1` alias, CORS, OpenAPI spec + drift test, token-login; `wtc-ui` image, CI `ui` job, compose+Helm `ui` service, `docs/setup/portal.md` |
| **P8 portal core views** | ✅ 2026-07-15 | dashboard (activity chart + per-env health + stats endpoints) + rich timeline (faceted filters, FTS, infinite scroll, saved filters, event drawer with inline `where`-journey); `/facets` + `actor` filter |
| **P9 change-intelligence views** | ✅ 2026-07-15 | `where` pipeline, `diff` env-matrix (new `/matrix` endpoint), service detail (current versions + freq/failure-rate/MTBF), alert correlation (`around`) in the event drawer |
| **P10 live + config surfaces** | ✅ 2026-07-15 | SSE live updates (`/api/v1/stream`, no-poll timeline/dashboard); Settings = source health + DB-backed editable rules/tag_patterns with hot-reload (`edit → next event re-routed`, no restart). Token management + multi-user auth stay out (RBAC non-goal) |
| **P11 ArgoCD ingest** | ✅ 2026-07-16 | fixture-first vs live Argo v3.4.5 on kind; canonical template ships the contract (4 template gotchas found live); per-sync-operation dedup keys (failed→succeeded retry = two rows); new `degraded` status (rank 3, upserts terminal rows); env tiers label>ns>name-suffix live-verified; full join proven live: github push INTENT → argocd APPLIED, 23h lag |
| **P12 GitLab ingest** | ✅ 2026-07-16 | SCM/CI-axis neutrality proof (GitHub↔GitLab, as Flux↔Argo was for GitOps); poller + `X-Gitlab-Token` webhook converge on shared dedup keys (`gl:pipeline`/`gl:mr`/`gl:push`); pipeline/MR/push normalizers + MR-diff enrichment; env inference via shared path rules. Verified live on a gitlab.com project: `wtc where` spans pipeline BUILD → MR merge INTENT → Argo CD APPLIED (private repo via Argo credential) |
| **P13 GitHub webhook completion** | ✅ 2026-07-17 | `/ingest/github` normalizes workflow_run/push/pull_request into the poller's Events + dedup keys (nested objects reuse the REST structs) — webhook + poller now peer modes, idempotent together; fixtures captured via the hook-deliveries API (no tunnel); onboarding gains the ingest-posture guide |
| **P14 Mapping webhook** | ✅ 2026-07-17 | `/ingest/webhook/<name>`: config-declared auth (static token XOR hex-HMAC) + payload→Event template mapping (same engine as `rules[].set`) + dedup_key template + rule facts; webhook names are first-class sources. Presets **Grafana + Jenkins** live-captured (Harbor/TFC deferred, capture-first doc covers them); doctor gains an unstable-dedup_key churn heuristic + mapping-error surfacing |
| **P15 Postgres backend** | ✅ 2026-07-17 | Opt-in `storage.backend: postgres` (pgx) → stateless wtc pod; one query surface via `?`→`$n` rebind + 5 explicit dialect branches (FTS→ILIKE, julianday→EXTRACT, GLOB→regex, pragma→pg_database_size; stats unified on substr); per-dialect migrations; `wtc migrate` (log output byte-identical across the copy); Helm bundled-postgres/external-DSN modes verified live on kind (no PVC, pod delete → zero loss, RollingUpdate); TestPG* parity suite + CI postgres service |
| **P16 Prometheus metrics** | ✅ 2026-07-17 | `/metrics` (promhttp) bearer-authed with `api_tokens`; ingest/dedup counters live in the single-writer path (complete across every source, zero per-handler wiring); suppression/mapping-error counters, poller last-success gauge, per-backend DB-size gauge, HTTP latency histogram (label = route **pattern**, not raw URL → no sha cardinality), SSE gauge. Optional separate **unauthenticated** listener (`metrics.listen`) for in-cluster scrapes. Helm ServiceMonitor (main-port-bearer XOR unauth-port models) + scrape-annotation toggle; `docs/setup/metrics.md`. ClickHouse rejected — change-event volumes never warrant it |
| **P17 Configuration tab** | ✅ 2026-07-18 | `/api/v1/config` extended with the redacted effective config (allowlist DTO built in serve.go — the server gains no new raw secrets; constant `"********"` masks; DSN → pgx-parsed host/port/db, creds stripped; sentinel guard test). Portal Settings → Configuration (source cards + doctor health chips, capture-mode warning, `/settings` redirect); mapping templates shown preset-resolved; `wtc config` CLI renders the same endpoint with the schedulers' effective defaults |
| **P18 Poller globs + Where links** | ✅ 2026-07-18 | Glob entries in `repos`/`projects` — shared dialect via exported `normalize.CompileGlob` + scope helpers (`SplitScope`/`ResolveScope`/`ScopeNamespace`), resolved every sweep; github filters its affiliation-bounded discovery (any glob form ok), gitlab gains scoped namespace discovery (`ListNamespaceProjects`, group→user 404 fallback — the rig project lives in a user namespace; fixture-captured live) with unscoped patterns fatal at config load. Where-page stage cards + drawer applied rows link to `event.url` (new tab, hover affordance, no dead links) |
| **Backfill window + multi-cluster docs** | ✅ 2026-07-18 | `sources.{github,gitlab}.backfill` overrides the first-poll history window (default 24h, unchanged); shown in Configuration tab + `wtc config`. `docs/setup/multi-cluster.md`: one central hub ingesting Flux/Argo from N clusters (per-spoke Provider/Alert → hub, `eventMetadata.cluster` identity, cluster→env rules, central SCM poller) — documents the flagship topology; no new code |
| **P20 incident correlation** | ✅ 2026-07-19 | `wtc blast <alert-id\|ts>` + portal "Likely causes": deterministic suspect ranking (recency 0–30, same-env +30 hard signal, same-service +20, kind weight, failed bump; **not** AI), direction flips on a change anchor (alerts after a deploy). New `/api/v1/blast` (`/around` shape didn't fit — cursor-paginated bare events); pure query, no schema change. Verified on the demo seed: prod api deploy top suspect at 69 ahead of same-service wrong-env (59) and the unrelated merge (16); reverse direction returns the alert |
| **P21 awareness (outbound)** | 📋 planned | Subscriptions: config `notifications[]` (glob-match → sink) emit on new rows **and status-changing upserts** (notify key = event id+status; new-row-only would never fire `status: failed` — trap #5); sinks slack/webhook/**grafana annotations**; own bounded queue off the write path (not the SSE broadcaster), at-least-once + retry + metric. Optional Atom feed. Largest lift; build last |
| **P22 harden the record** | 📋 planned | `wtc export` (range → CSV/NDJSON, streaming `/api/export`), `wtc backup` (server-side `VACUUM INTO` streamed over `/api/backup` + litestream docs), `wtc explain <id>` (per-field rule trace; needs a nullable `facts` column captured at ingest — Facts are not reconstructible from stored payloads). Trust/compliance |

Unplanned addition: `demo/` — three dummy services + fake three-cluster Flux
wiring generating real events continuously (operator-requested test bed;
doubles as the P3 live-acceptance rig and portfolio demo).

**Direction change (2026-07-14):** the operator wants a richer, portal/
platform-style UI with dashboards and metrics, in addition to — not instead of
— the embedded timeline. Two UIs are kept: the toolchain-free built-in (P5,
zero-dependency, served from the binary) stays as a lite fallback; a separate
SPA portal (P7–P10) is the primary rich experience. This reverses several
CLAUDE.md hard decisions (toolchain-free-only, no-SPA, no dashboards,
single-binary UI) — those are updated with operator approval. Mobile-web and
single-binary embedding are explicitly NOT requirements for the portal.

## Decisions (operator answers, 2026-07-12 — previously "open questions")

| # | Decision |
|---|---|
| Q1 | Image tags embed the git sha: `sha-<shortsha>` and `<semver>-<sha>` both occur. Ship a configurable `tag_patterns` list with those two as defaults (SPEC §2) so other conventions work without code changes. |
| Q2 | Cluster-per-env: `dev`, `staging`, `prod`. Default mapping cluster name = env name. All run Flux. |
| Q3 | No public endpoint in v1 — wtc runs inside the org's private network (EKS or similar). GitHub API **poller is the primary GitHub ingest** and moves from P4 into P1; webhook path still built for public-endpoint deployments. Flux ingest unaffected (in-cluster). |
| Q4 | Microservices: one repo per service; manifests live in each repo under `./infrastructure` (configurable `infra_path`), kustomize layout: `infrastructure/base/**` + `infrastructure/overlays/<env>/**`. Default env inference: `infrastructure/overlays/<env>/**` path rules. |
| Q5 | Terraform runs in CI only — `wtc wrap` terraform support targets non-interactive CI usage (`WTC_SERVER`/`WTC_API_TOKEN` env config). |
| Q6 | Flux v2.x confirmed: notification-controller with Provider/Alert CRDs, `generic-hmac`, image-automation on dev. |
| Q7 | Packaging: Helm chart (in-cluster, primary) + docker-compose (VMs/local). No systemd unit in v1. |

**Decision update (2026-07-16, operator):** Q3's "no public endpoint" describes
the operator's own deployment, not the product. wtc is designed **as if
reachable from anywhere** — each installation chooses its exposure. Consequences:
GitHub webhook mode is completed as a first-class peer of the poller (P13); the
mapping webhook (P14) may target SaaS senders; the poller remains the
recommended default for private deployments and keeps its sweeper role
regardless.

## Phase 0 — Skeleton (foundation)

Repo init, Go module, Makefile, CI (GH Actions: test+lint+build). Cobra skeleton with `serve`, `record`, `log`, `init`, `doctor` stubs. Config loader + `${VAR}` expansion + env overrides. SQLite store: pragmas, embedded migrations, single-writer goroutine + ingest channel, read pool. `POST /ingest/generic`, `GET /healthz`, `GET /api/events` (basic filters). `wtc record` and `wtc log` end-to-end.

**Accept:** `wtc serve` + `wtc record --kind manual --env dev --service api --title test` + `wtc log --since 1h` round-trips; duplicate record with same dedup_key does not duplicate; `go test ./...` green including store tests on temp DB.

## Phase 1 — GitHub ingest (poller-primary + webhooks)

**GitHub API poller** (primary, per Q3): per-repo high-water mark persisted in DB; lists workflow runs, merged PRs, default-branch commits; normalizes through the same pipeline as webhooks; doubles as the webhook-loss sweeper; bounded first-run backfill. HMAC middleware. Capture mode (`--capture-dir`, captures webhook bodies and poller API responses). Parsers + normalizers for `workflow_run`, `push`/commits, `pull_request` (closed+merged) — webhook and REST shapes both covered. Rules engine per SPEC §3 with fact map (repo, branch, event, paths, actor…). Status-upsert lifecycle for workflow_run. Redaction pass. `doctor` counts real data.

Fixture strategy without a public endpoint: capture poller API responses live; webhook payload fixtures via GitHub's hook-deliveries API or a temporary tunnel session. *Operator decision 2026-07-13: webhook fixtures skipped for now — P1 normalizers target REST shapes only; `/ingest/github` stays HMAC+capture until webhook fixtures exist (envelope parsing follows the same fixture-first rule when a public endpoint materializes).*

**Accept:** golden-fixture tests for ≥6 captured real payloads across poller + webhook shapes (build started/success/failure, flux-bot image-automation commit, human infra commit, merged PR); truncated-paths case lands `env=""` not misrouted; live: operator's repos wired via poller (api_token only, no public endpoint), events visible in `wtc log` within one poll interval; running the poller twice or replaying the same webhook delivery yields one row.

## Phase 2 — Flux ingest

`/ingest/flux` with generic-hmac verification. Parse notification events from **captured fixtures first** (do not trust remembered field names). Dedup `(object, revision, reason)` + suppression window. Map severity → status. Cluster identification: cluster-per-env (Q2), cluster name = env name by default, carried via Alert `eventMetadata` (validate against fixtures). Rules mapping Kustomization/HelmRelease/ImageUpdateAutomation → service/env. Ship `docs/setup/flux-provider.yaml`.

**Accept:** fixtures for reconcile success, reconcile failure, image-automation commit; a reconcile loop firing N identical events stores 1 row; dev-env flow visible end-to-end in `wtc log --env dev`: flux-bot push (github) → reconcile (flux); prod flow: human PR merge → reconcile, both rows carrying the same manifest revision.

## Phase 3 — The three killer queries

`where` (BUILD → INTENT → APPLIED per SPEC §6; tag↔sha resolution via `tag_patterns`, defaults `sha-<shortsha>` and `<semver>-<sha>` per Q1), `diff`, `handoff`, FTS5 `-q`. GitHub PR-diff enrichment (SPEC §7) if `api_token` present — this is what links tag bumps to manifest revisions. Seeded-fixture timeline for deterministic query tests.

**Accept:** on a seeded timeline reproducing the operator's real flow (build → PR bump → merge → reconcile in staging, later prod), `wtc where <sha>` shows both envs with correct intent→applied lag; `wtc diff staging prod` flags the not-yet-promoted service; `handoff` renders correct counts; every query has `--json` golden tests.

## Phase 4 — Gap closers

`wtc wrap` with helm/terraform sniffers (SPEC §5) covering feature-branch installs and terraform-in-CI (Q5: non-interactive, config via `WTC_SERVER`/`WTC_API_TOKEN`). Revert-PR heuristic → kind=rollback. `docs/setup/gha-report-step.md` (optional — operator tags embed the sha; kept for orgs whose tags don't). Packaging per Q7: Dockerfile + Helm chart under `deploy/helm/` + `deploy/docker-compose.yaml`.

**Accept:** `wtc wrap -- helm upgrade pr-123 ./chart -n pr-123` records started→succeeded with service/namespace inferred; killing serve, generating GitHub activity, restarting, and letting the poller catch up backfills with zero duplicates; wrap with server down still runs the wrapped command and exits with its code; helm chart installs into a kind/k3s cluster and serves; compose file boots locally.

## Phase 5 — Surfaces

Embedded web timeline (toolchain-free per CLAUDE.md): filter bar (env/service/kind/text), day-grouped stream, event → source deep link. Alertmanager ingest + `wtc around <ts|alert>` (changes in ±window before an alert). Slack digest: `wtc handoff --slack-webhook <url>` + optional cron in serve.

**Accept:** UI served from the single binary, usable on mobile width, no external asset downloads; alert fixture correlates to the deploy that preceded it; digest posts to a real Slack webhook.

## Phase 6 — Release hygiene ✅

goreleaser (linux/darwin, amd64/arm64), versioned migrations check, README with 5-minute quickstart, demo seed command (`wtc demo` loads a synthetic week), retention job verified, basic load sanity (10k events: log/diff < 100ms).

**Delivered:**
- Auto commitizen versioning in CI (tags `vX.Y.Z`, bumps Helm appVersion) + multi-arch images (linux/amd64+arm64).
- **goreleaser** — `.goreleaser.yaml` + `release.yml` fire on `v*` tags, attaching static cross-platform binary archives (linux/darwin × amd64/arm64, no CGO) + checksums to the GitHub Release. Demo services tag `demo-<svc>-v*` and are excluded.
- **Retention job** — opt-in `retention:` config; single-writer DELETE (normal vs `pr-*` ephemeral windows) + `incremental_vacuum`; prunes on start then every `interval`; table-driven test covers normal/ephemeral/unmapped splits + FTS consistency. `wtc doctor` surfaces `oldest_event`.
- **`wtc demo`** — seeds a synthetic week via `/ingest/generic` (CLI never opens the DB); showcases `log`/`where`/`diff`/`around` + drift with zero real wiring.
- **Load sanity** — `TestLoadSanity`: 10k events, `log`/`diff` medians under the 100ms budget (observed ~0.2ms / ~15ms).
- **LICENSE** — Apache-2.0 text added (repo declared it, file was missing).

Config gained standalone `d`/`w` duration suffixes so `keep: 180d` reads as the SPEC intends.

---

# UI Platform track (P7–P10)

A separate rich UI, built and deployed independently of the binary. The
existing embedded timeline (P5) is kept unchanged as a dependency-free lite
view; this track is additive.

## Architecture decision (do not relitigate without operator approval)

- **No new backend.** The Go binary stays the single backend/API and sole
  owner of SQLite/ingest/queries. The portal is a **client** of `/api/*`.
  Adding a second backend language would fragment ownership of the data
  plane and break the "one self-hosted binary for the data" property.
- **Frontend stack:** React 18 + TypeScript + Vite; Tailwind CSS + shadcn/ui
  (Radix-based, copy-in components — no heavyweight framework lock, fits the
  vendor-neutral ethos); TanStack Query (server state) + a router; Recharts
  for charts. A real toolchain (node/npm/bundler) is allowed **for the `ui/`
  tree only** — it never touches the Go build.
- **Typed client:** the Go server emits an OpenAPI spec (`/api/openapi.json`);
  the portal generates its API client from it, so the contract can't drift.
- **Deploy:** the portal builds to static assets served by its own container
  (nginx) or any static host/CDN; separate from the wtc pod. Optionally the
  built `dist/` may be `go:embed`-ed for a single-image convenience deploy,
  but that is not a requirement. No mobile-web requirement.
- **Auth for the portal:** start with a token-login screen (enter an
  `api_tokens` value → stored client-side → sent as bearer). Real multi-user
  login/RBAC is a possible later phase, still gated on the v1 RBAC non-goal
  being lifted.
- **Repo layout:** new top-level `ui/` (its own `package.json`, gitignored
  `node_modules/`, `dist/`). CI gains a `ui` job (lint + typecheck + build)
  and, on main, publishes `ghcr.io/migueljfsc/wtc-ui`.

## Phase 7 — Portal foundation

Scaffold `ui/` (Vite + TS + Tailwind + shadcn + TanStack Query, app shell with
nav + theming). API hardening: versioned `/api/v1` namespace (alias current
routes), configurable CORS middleware (allowed origins in config; off by
default), an OpenAPI spec endpoint, and token-login flow. `ui/` CI job +
`ghcr.io/migueljfsc/wtc-ui` image; docker-compose + Helm gain a `ui` service.

**Accept:** portal shell runs against a live wtc, authenticates with an API
token, navigates between empty view stubs; CORS lets the separately-served
SPA call the API; `docs/setup/portal.md` shows wiring both containers.

## Phase 8 — Portal core views

- **Dashboard/overview:** activity summary (events over time), deploy
  frequency + failure-rate tiles per env, env-health cards, recent changes
  feed. Needs new aggregation endpoints (`/api/v1/stats/...` — time buckets,
  per-env/per-service counts) — these are the DORA-ish metrics now in scope.
- **Timeline view:** the rich log — faceted filters (env/service/kind/status/
  actor/text), saved filters, infinite scroll, an event-detail drawer showing
  the full (redacted) payload and the event's `where`-journey inline.
- **Global search** over events (FTS).

**Accept:** dashboard renders real metrics from the demo stack; timeline
filters/searches without a page reload; event drawer shows a change's journey.

## Phase 9 — Change-intelligence views

- **`where` visualized:** the BUILD → INTENT → APPLIED journey as a per-env
  horizontal pipeline with intent→applied lag, unknown/gap markers.
- **`diff` visualized:** a services × environments matrix, drift highlighted,
  not-yet-promoted services flagged, revision-only caveats surfaced.
- **Service detail pages:** current version across every env, deploy history,
  MTBF/lead-time, recent failures.
- **Alert correlation (`around`) visualized:** a timeline centred on an alert
  with the preceding window of changes highlighted.

**Accept:** the three killer queries + alert correlation are each answerable
entirely in the UI, driven by the live demo data; a promotion visibly moves a
service across the env matrix.

## Phase 10 — Live + config surfaces

- **Live updates:** an SSE stream endpoint (`/api/v1/stream`) so the timeline
  and dashboard update without polling.
- **Config UI:** view/edit the env/service inference rules and `tag_patterns`,
  view source health (`doctor`) as a page, manage tokens. (Editing rules
  implies a writable config path — decide file-backed vs DB-backed then.)
- **Optional:** real multi-user auth — only if the RBAC non-goal is lifted.

**Accept:** events appear in the portal live (no refresh); an operator edits a
rule in the UI and sees a subsequently-ingested event re-routed.

## Sequencing notes

- P0→P1→P2 strictly ordered. P3 needs P1+P2 data shapes. P4 parallelizable after P1.
- Capture fixtures the moment real sources are wired (P1/P2) — they are the contract for everything downstream.
- Q3 re-order applied: the GitHub poller (formerly P4 sweeper) is now in P1 as the primary ingest path; HMAC middleware still built in P1 (webhook mode stays supported; Flux traffic is in-cluster/private anyway).
- **UI track (P7–P10)** is independent of the remaining P6 work and can proceed in parallel. P7 is the gate — the OpenAPI spec + `/api/v1` + CORS + login unblock everything after it. P8/P9 need only P3-era query data (already present); P9 leans on the aggregation endpoints added in P8. P10 (live/config) last.
- Portal work must not regress the embedded timeline or the CLI; the Go API is extended additively (new `/api/v1` routes; existing `/api/*` and `/` stay).
- **ArgoCD ingest (P11)** is a data-plane phase parallel to P2 (Flux), independent of the UI track. It reuses the existing dedup/suppression + rules engine + `where` revision join, adding only a new parser, ingest route, and a canonical notification template. Fixture-first against a stood-up Argo instance — not the operator's Flux-only clusters. Once landed, the portal's `where`/`diff`/matrix views render argocd deploys with no UI change (same Event schema).

---

# Additional GitOps ingest (post-v1)

Vendor neutrality is the product (CLAUDE.md mission). Flux and Argo CD are the two dominant GitOps CD controllers; shipping Argo CD ingest alongside Flux is the neutrality proof. The operator's own stack is Flux-only, so this phase is fixture-driven against a stood-up Argo instance, not their clusters.

## Phase 11 — ArgoCD ingest

`/ingest/argocd` accepting Argo CD **notifications-controller** webhooks. Fixture-first: stand up Argo CD (local kind), wire its notifications config at wtc, capture real payloads, freeze fixtures, then implement.

**Define the contract — don't parse Argo's native shape.** Argo CD's notification body is *operator-templated* (`argocd-notifications-cm` decides the JSON); there is no fixed webhook schema like Flux's eventv1. So wtc **ships** the contract: `docs/setup/argocd-notifications.yaml` with a canonical template emitting a stable shape — `app`, `project`, `revision` (sync git sha), `syncStatus`, `healthStatus`, `operationPhase`, `destServer`, `destNamespace`, `repoURL`, `sourcePath`, `targetRevision`, `startedAt`, `finishedAt`, `triggeredBy`. The parser targets that shape; the doc is the wiring the operator must apply for the parser to work.

**Trigger → kind/status map.** `on-sync-succeeded`/`on-deployed` → kind=deploy, status=succeeded; `on-sync-failed` → deploy/failed; sync-running → started; `on-health-degraded` → status carries health (correlate like an alert). Status-upsert lifecycle as everywhere: one row per `(app, revision)`, transitions update in place.

**Dedup + suppression.** Argo re-notifies on every resync/refresh — same spam trap as Flux (known-trap #1). Dedup `argocd:<app>:<revision>:<phase|healthReason>`; drop repeats within the suppression window (reuse Flux's `suppression_window`).

**Inference differs from Flux — do NOT reuse cluster=env.** One Argo instance manages many clusters, and its "cluster" is a destination *server URL*, not an env. Fact map carries app, project, destNamespace, destServer, sourcePath, app labels — env/service inference runs off **app-name pattern / destination namespace / an `env` app label / source path**, never off cluster name. Ship a default `rules` example for Argo (mirroring the Flux block but keyed on app/namespace). Unmatched → `env=""`, surfaced by `doctor` (known-trap #2 unchanged).

**tag↔sha join stays intact.** Argo's sync `revision` is the manifest-repo git sha (multi-source apps 2.6+ expose `revisions[]` — capture all) → feeds `where` APPLIED exactly like a Flux reconcile revision. A commit traced through GitHub INTENT lands on Argo-applied envs the same way; `wtc where <sha>` then spans Flux- and Argo-applied envs in one picture.

**Companion changes this phase carries** (per the per-phase definition of done): SPEC §1 `source` enum + dedup_key table gain `argocd`; §2 config gains a `sources.argocd` block; §4 HTTP surface gains `POST /ingest/argocd`; §9 docs/setup gains `argocd-notifications.yaml`; CLAUDE.md operator/known-traps note Argo is fixture-supported but not the operator's stack.

**Accept:** golden fixtures for sync-succeeded, sync-failed, health-degraded (+ app-of-apps if present); a resync loop firing N identical notifications stores 1 row; an Argo-managed flow visible end-to-end — PR merge (github) → Argo sync (argocd) carrying the same manifest revision — in `wtc log`; `wtc where <sha>` shows the Argo-applied env with correct intent→applied lag alongside Flux envs; `wtc diff`/`doctor` treat argocd deploys identically to flux; operator can wire a real Argo instance using only `docs/setup/argocd-notifications.yaml`.

**Resolved (operator answers, 2026-07-16):**
- Auth: **static shared-secret header** `X-WTC-Token` (`sources.argocd.webhook_secret`), constant-time compare. Argo templates can't body-HMAC; in-cluster traffic bounds the exposure.
- Health-degraded: **kind=deploy with new `degraded` status** (rank 3, outranks the terminal pair so it wins the upsert on the completed operation's row); recovery is visible on the next revision's row; doctor/dashboards surface it.
- Env default rule: **ordered tiers, all three shipped** — `env` app label > destination namespace > `<service>-<env>` name suffix; unmatched → `env=""` per trap #2.

**Follow-ups (filed 2026-07-16, not blocking):**
- Fixture gaps: capture `revisions[]` (needs a multi-source Application) and a populated `triggeredBy` (needs an authenticated argocd-cli/UI sync) — extend the goldens when captured.
- Demo stack: the kind rig's Argo apps sync manually; consider `syncPolicy.automated` + pointing Argo at the remaining demo overlays so the demo continuously exercises both GitOps engines.

**Live-validation addenda (found in stage 3, fixed same day):**
- Row keys are per sync **operation** (`argocd:<app>:<revision>:<startedAt>`), not per (app,revision): with equal-rank terminal statuses, a revision-keyed row made a failed→succeeded retry of the same revision permanently invisible. A retry is a new logical change (trap #5); Flux gets the same separation via `reason` in its key.
- Argo's notified-annotation gating cuts both ways: it re-fires on observed state flips (suppression window handles the spam) but sends nothing for transitions it never observed — a same-revision resync can be legitimately missed; documented as an Argo-side limitation (known trap #10).

---

# Ingest breadth track (P12–P14, operator decision 2026-07-16)

Ingest strategy by layer: **parsers for the top two per category** (SCM/CI:
GitHub + GitLab; GitOps: Flux + Argo — done), a **mapping webhook for the long
tail** (any tool that POSTs JSON becomes config, not code), and
`/ingest/generic` when the operator owns the sender. Underpinned by the
reachability decision update above: wtc is designed as reachable from anywhere.

## Phase 12 — GitLab ingest

**Shipped 2026-07-16.** Poller + `/ingest/gitlab` webhook, both converging on
`gl:pipeline`/`gl:mr`/`gl:push` dedup keys; pipeline/MR/push normalizers +
MR-diff enrichment; env inference via the shared path-glob rules. Golden
fixtures (7 API + 3 webhook) with poller-twice + webhook-replay idempotency;
capture helper extracted to `internal/capture` to keep ingest packages free of
a `server` import. Verified live on a gitlab.com project: `wtc where
sha-<sha>` spans pipeline BUILD → MR merge INTENT → Argo CD APPLIED (private
repo pulled via an Argo repository credential). See
[docs/setup/gitlab.md](setup/gitlab.md).

The SCM/CI-axis neutrality proof, mirroring the P11 playbook: fixture-first
against a stood-up instance (gitlab.com free project or docker `gitlab-ce`; the
operator's stack is GitHub-only, so this is not their infra).

Poller parity with GitHub (SPEC §7 shape): per-project watermarks over
pipelines, merged MRs, default-branch commits; bounded first-run backfill;
doubles as the webhook-loss sweeper. Webhook receiver `/ingest/gitlab`
verifying GitLab's static `X-Gitlab-Token` header (constant-time; GitLab does
not HMAC-sign — same auth shape as argocd) covering Pipeline / Push / Merge
Request hooks. Status-upsert lifecycle keyed on pipeline id (same trap-#5 shape
as `workflow_run`). MR-diff enrichment via the MR changes API (the §7 analog
that links tag bumps to manifest revisions). Facts/rules parity: repo, branch,
event, paths, actor. Dedup keys `gl:…` added to the SPEC §1 table.

**Accept:** golden fixtures ≥6 across poller + webhook shapes (pipeline
started/success/failure, merged MR, push, unknown-paths case → `env=""`);
poller-twice / webhook-replay = one row; a GitLab-hosted flow visible
end-to-end — pipeline → MR merge → Flux or Argo applying the same revision —
with `wtc where` spanning it; `docs/setup/gitlab.md` wires a real project using
only the docs.

## Phase 13 — GitHub webhook completion (reachability posture)

**Shipped 2026-07-17.** `/ingest/github` normalizes workflow_run/push/
pull_request into the poller's Events + dedup keys (nested resource objects
reuse the REST structs; only `push` needed a shared `pushEvent` builder).
Webhook + poller are now peer modes, idempotent together. Fixtures captured
from real deliveries on `migueljfsc/wtc` via the hook-deliveries API (no
tunnel). `github-webhook.md` is a full wiring guide; onboarding gains the
ingest-posture section. The github poller now captures via `internal/capture`
(no `server` import), matching gitlab.

Per the 2026-07-16 decision update: exposure is per-installation, so the
P1-deferred webhook envelope parsing lands and the poller/webhook pair becomes
two peer modes of one source.

Capture real webhook payloads via the **hook-deliveries API** — a registered
hook records delivery request bodies even when its target URL 404s, so no
tunnel is needed — freeze fixtures, then parse envelopes for `workflow_run` /
`push` / `pull_request` into the existing normalizers. REST and webhook shapes
must converge on the same Events and dedup keys, so both modes can run
simultaneously (webhooks for latency, poller as the loss-recovery sweeper —
idempotent by design). Docs rework: `github-webhook.md` graduates from
capture-only to full wiring; a posture guide (private → poller-primary;
public → webhooks + sweeper) added to onboarding.

**Accept:** webhook fixtures for the three event families; webhook replay +
poller double-ingest = one row each; a live delivery lands in `wtc log` within
seconds where the poller alone would take a poll interval; both modes running
together produce zero duplicates over a real day of activity.

## Phase 14 — Mapping webhook (long-tail ingest)

**Shipped 2026-07-17.** `/ingest/webhook/<name>` with config-declared sources
(`sources.webhooks[]`): auth (static token XOR hex-HMAC), a payload→Event field
mapping compiled from Go templates over the parsed JSON body (reusing the
rules-engine funcs + `default`), a required stable `dedup_key` template, and
optional facts feeding the rules engine. Each webhook name is registered as a
first-class `model.Source`, so it shows under its real name in log/facets/
doctor. Presets `grafana` + `jenkins` are live-captured golden fixtures
(Grafana 11.3 test contact point; Jenkins Notification Plugin serialized via its
own `buildJobState` — its SSRF guard blocks a private-IP POST). doctor gains a
per-source unstable-dedup_key **churn heuristic** (rows sharing title/kind/
status seconds apart under distinct keys) and surfaces mapping-template eval
failures (rejected `422`, never dropped). `/ingest/generic` stays separate.
Harbor/TFC presets deferred; `docs/setup/mapping-webhook.md` wires a novel tool
capture-first. See below for the original plan.

`/ingest/webhook/<name>` — operator-declared sources in config: auth (static
token header, or HMAC where the sender signs), a payload→Event field mapping
(go-template over the parsed JSON body — the same template engine rules `set:`
uses), a `dedup_key` template, and optional facts feeding the rules engine.
Mapped events enter the standard pipeline (redaction → rules → status-rank
upsert), so lifecycle transitions work when a sender emits phase updates.

Shipped **presets** — mapping + fixtures, tested like any parser: Grafana
alerting, Jenkins notification plugin, Harbor, Terraform Cloud run
notifications (SaaS senders in scope per P13's posture). Capture mode is the
authoring loop for novel tools: point the tool at wtc, read the captured body,
write the mapping.

Guardrails: `dedup_key` templates are the footgun (an unstable key silently
breaks idempotency) — doctor gains a per-webhook-source duplicate-churn
heuristic; template eval errors surface in doctor, never guessed. Decide at
build time whether `/ingest/generic` folds in or stays separate (leaning
separate — it is the "you own the sender" path and needs no mapping).

**Accept:** an unmodified third-party payload (fixture) ingests via a
config-only mapping with correct kind/status/dedup lifecycle; a deliberately
unstable dedup_key template is flagged by doctor; ≥2 shipped presets validated
against live tools; `docs/setup/mapping-webhook.md` lets an operator wire a
novel tool using only capture mode + the doc.

### Sequencing (P12–P14)

Independent of each other and of the UI track. P13 is the smallest and touches
only existing github code — good gap-filler. P12 is the biggest neutrality win
and next by default. P14 last: its presets benefit from the P13 posture docs,
and doctor's churn heuristic is easier once two SCM sources exercise it.

# Storage & operations track (P15–P16, operator decisions 2026-07-17)

Theme: run wtc like a production service. Separate the data from the pod
(opt-in Postgres → stateless wtc Deployment), then make the service observable
(Prometheus metrics). Decisions recorded 2026-07-17:

- **Postgres** is the second storage backend (`jackc/pgx/v5` approved —
  pure Go, no CGO conflict). Selected via an **explicit `storage.backend` key**
  (no DSN sniffing).
- **SQLite stays the default.** The single-binary story is the product
  differentiator; Postgres is opt-in for k8s/production posture. This amends
  the CLAUDE.md storage hard decision and removes "Postgres backend" from the
  non-goals list (operator approval 2026-07-17).
- **Helm bundles an optional postgres** behind `postgresql.enabled`; the wtc
  chart takes a DB URL input — auto-wired to the bundled pod's service FQDN
  when enabled, a `localhost` placeholder for BYO-DB otherwise.
- **Single replica stays** — this phase separates the data, nothing else.
  Pollers, suppression windows, and the digest are per-pod; HA/leader-election
  is out of scope. Idempotent ingest makes an accidental second replica
  safe-but-wasteful, not supported.
- **Metrics are their own phase (P16). ClickHouse is rejected**: change events
  run 10³–10⁵/day worst case, ClickHouse earns its keep around 10⁹ rows; DORA
  aggregates over years of ledger compute in milliseconds on either backend.
  A ClickHouse pod would cost ~1 GB RAM baseline, a third SQL dialect, and an
  ops surface that undercuts the lightweight-neutral positioning. Revisit only
  if wtc ever ingests high-cardinality telemetry — a stated non-goal.

## Phase 15 — Postgres backend (stateless wtc pod)

**Shipped 2026-07-17.** As planned below, with two findings worth recording:
postgres rejects unqualified stored-row columns in `ON CONFLICT DO UPDATE`
(42702) — the shared upsert now qualifies them (`events.<col>`), which sqlite
also accepts; and stats bucketing turned out to need no branch at all —
`substr` over the fixed-width ts text replaced the sqlite-only `strftime` for
both dialects. Live-verified on kind: bundled-postgres install has no wtc PVC,
survives pod deletion with the ledger intact, and upgrades via RollingUpdate
(an init wait was added so first boot doesn't race the DB); `wtc migrate`
produced byte-identical `wtc log` output across the sqlite→pg copy. Helm
secrets were consolidated (operator feedback): one chart-wide `existingSecret`
with opinionated keys covers API tokens + DB auth (`WTC_PG_PASSWORD` /
`WTC_STORAGE_DSN`); the DSN lands in the ConfigMap referencing
`${WTC_PG_PASSWORD}`, expanded by wtc's own loader — both modes live-verified.

The driver is **operational posture, not scale** — SQLite handles these
volumes for a decade. What Postgres buys in k8s: the wtc pod becomes
disposable (no RWO PVC → RollingUpdate instead of Recreate, instant reschedule
on node loss), standard backup/restore, managed offerings (RDS, CloudNativePG).

**Config.** New `storage:` section: `backend: sqlite` (default; keeps using
`server.db` as the file path — existing configs unchanged) or
`backend: postgres` + `storage.dsn` (required then; empty DSN is a startup
error, fail fast). `WTC_STORAGE_BACKEND` / `WTC_STORAGE_DSN` env overrides.
The DSN carries credentials → inject via the existing helm secretRef pattern,
never a plain chart value.

**Store.** One `Store` struct gains a dialect: per-dialect embedded migration
dirs (`migrations/sqlite/` — the existing four, unchanged; `migrations/
postgres/` — fresh port), a small `?`→`$n` rebind helper, and ports of the
~26 dialect-specific sites (audited 2026-07-17): `julianday` → `EXTRACT(EPOCH)`,
`GLOB` → regex/`LIKE`, `pragma` size queries → `pg_database_size`,
`INSERT OR …` → `ON CONFLICT`. FTS5 (`wtc log -q`) → plain `ILIKE` on postgres
initially — the events table is small; `pg_trgm` only if it ever hurts. The
single-writer goroutine and write/read pool split stay on both backends
(harmless on pg, keeps ordering semantics identical).

**Ledger migration.** The operator has real history; poller backfill is
bounded and would lose it. One-shot offline command (exact name at build time,
e.g. `wtc migrate --to <dsn>`): copies events, poll watermarks, and DB-backed
config overrides from the sqlite file into postgres; serve stopped; idempotent
re-run safe (dedup keys make event copies upserts).

**Helm.** `storage.backend=postgres` → wtc Deployment drops the PVC and
switches to RollingUpdate. `postgresql.enabled=true` → minimal bundled
postgres (StatefulSet + PVC + Secret + Service) with the wtc DSN auto-wired to
its FQDN; `enabled=false` → `externalDatabase.url` (default `localhost`
placeholder the operator overrides; docs point at CloudNativePG/RDS).
`storage.backend=sqlite` keeps today's chart behavior exactly. docker-compose
gains a postgres variant.

**Tests.** Store suite parameterized over backends: sqlite always; postgres
gated behind `WTC_TEST_PG_DSN` locally and a postgres service container in CI.
E2E smoke (fixture replay → log/where/diff) runs on both.

**Accept:** `go test ./...` green on both backends in CI; `helm install` with
`postgresql.enabled=true` yields a wtc pod with **no PVC** that survives
`kubectl delete pod` with zero data loss and upgrades via RollingUpdate;
`wtc migrate` moves a real sqlite ledger and `log`/`where`/`diff` output is
identical before/after; the sqlite default path is byte-for-byte unchanged for
existing installs; `docs/setup/postgres.md` wires it using only the docs.

## Phase 16 — Prometheus metrics

**Shipped 2026-07-17.** As planned below, with the two build-time decisions
resolved: `prometheus/client_golang` approved (own registry, never the global
default), and the optional unauthenticated listener **built** (`metrics.listen`
/ `WTC_METRICS_LISTEN`; off by default) — an api_token also grants `/api/*`
config writes, so an in-cluster least-privilege scrape path was worth the ~25
lines. Two implementation notes worth recording: the ingest/dedup counters live
in `Store.Ingest`'s single-writer reply path, not the HTTP handlers, so every
source (webhooks, both pollers, generic, mapping) counts with no per-handler
wiring and the two can never drift; and the HTTP histogram's `path` label is the
Go 1.22 mux **route pattern** (`r.Pattern`, method prefix stripped), never the
raw URL — raw `/api/v1/where/<sha>` paths would explode cardinality. The
`wtc_db_size_bytes` gauge is a scrape-time collector reusing the doctor size
query (`Store.SizeBytes`), emitting nothing on error rather than a lying zero.
Helm ServiceMonitor guards the main-port model behind a `required`
`existingSecret` (the scrape needs `WTC_API_TOKEN`); the unauth model keys off
`metrics.port`, which the chart injects as `metrics.listen` into the ConfigMap
so the container port and process listener can't drift.

`/metrics` via `prometheus/client_golang` (dep to approve at build time).
Instruments: `wtc_ingested_total{source}`, `wtc_deduped_total{source}`,
`wtc_suppressed_total{source}`, `wtc_mapping_errors_total{source}`, poller
last-success/lag gauges per repo, DB size gauge (per-backend query), HTTP
request duration histogram `{path,method,status}`, SSE connection gauge.

Exposure: wtc may be public (P13 posture) — `/metrics` leaks source names and
activity levels, so it is **bearer-authed with the existing `api_tokens`**
(Prometheus scrape configs support authorization headers natively); decide at
build time whether to also offer a separate unauthenticated listen addr for
in-cluster-only setups.

Helm: optional ServiceMonitor + scrape-annotation toggle. Docs:
`docs/setup/metrics.md` with a scrape config and example alerts (source
silent > N, mapping errors > 0, poller lag).

**Accept:** a real Prometheus scrape ingests the endpoint; counters proven by
fixture replay (N ingested → counter = N, replay again → deduped counter
moves); ServiceMonitor verified on a kind cluster with kube-prometheus-stack.

### Sequencing (P15–P16)

P15 first — P16's DB-size gauge and CI wiring benefit from the backend split
landing beforehand. Both independent of the ingest and UI tracks.

# Operator visibility (P17, operator-requested 2026-07-18)

## Phase 17 — Configuration tab (effective-config visibility)

**Shipped 2026-07-18.** As planned below with the four operator decisions
(Settings renamed, templates in full, both CLI + portal, masked secrets)
applied. Implementation notes: the view lives in `internal/config` (`View` +
`NewView`) so the sentinel test sits next to the struct it guards; preset
resolution got an exported `mapping.Resolved` (pass-through on unknown presets
— Compile stays the place that errors, and the view always runs after Compile
succeeded); string lists normalize nil→`[]` so clients index without null
checks (caught live: `projects: null` would have crashed the tab); retention/
digest sections report the SCHEDULERS' effective defaults rather than raw
zeros, and whole-day durations render as `"180d"` matching the config's own
d-suffix syntax. Follow-up in the same phase (operator-requested): a real
**Settings tab** — API version via bearer-authed `GET /api/v1/version`
(version strings fingerprint deployments, so not public), build-time UI
version, connection status, theme, and session/local-data controls.

**Problem.** The portal shows only rules + tag_patterns (`/api/v1/config`,
P10). Everything else the daemon runs with — which ingest paths are wired
(github poller/webhook, gitlab, flux, argocd, mapping webhooks), storage
backend, retention, digest, metrics posture — is invisible without reading the
ConfigMap or pod env. Operator wants a **Configuration tab**: one place that
answers "what does this wtc have configured?".

**The hard requirement: secrets never leave the server.** The config carries
webhook secrets, HMAC keys, PATs, the DSN. Exposure rules (operator decisions
2026-07-18):

- **Fixed-mask strings** for every secret: a configured secret renders as the
  constant `"********"`, an unconfigured one as `""` — never values, never
  partials (last-4 leaks entropy), and the mask is **constant-length
  regardless of the real value's length** (length is information too).
  `api_tokens` → a list of masks (count is visible, values are not).
- **Allowlist DTO, never struct marshal.** The view is an explicit
  hand-written struct built field-by-field from `config.Config`; forgetting a
  new config field fails SAFE (not exposed) instead of leaking it. Marshalling
  `config.Config` with omissions is forbidden.
- **Sentinel guard test** (the phase's must-have): build a fully-populated
  `config.Config` where every secret field holds a sentinel string, marshal
  the view, assert the sentinel never appears anywhere in the JSON (masks
  appear instead).
- Postgres DSN → parsed **host/port/database only** (creds stripped via
  `pgx.ParseConfig`), password position masked; omit entirely if the parse
  fails. Capture-dir set → surfaced with a warning badge (it is a
  data-exposure flag).

**API.** Extend the existing `GET /api/v1/config` response (additive, one
"effective config" endpoint, portal already has the query hook) with the
redacted sections: `server` (listen, cors, capture flag), `storage`, `auth`
(token count), `sources` (github/gitlab/flux/argocd params + per-webhook
name/preset/auth-mode/templates), `digest`, `retention`, `metrics`. Values are
**effective** (post `${VAR}` expansion + `WTC_*` overrides) — built once in
`serve.go` (the only holder of the full config) and passed into
`server.Options` as a prebuilt view, so the Server gains no new raw secrets.
Static snapshot: sources config cannot change without a restart (rules/tags
keep their live-edit path unchanged). Mapping-webhook templates are shown in
full — operator-authored config-as-code, same exposure class as rules.
`openapi.json` updated + `npm run gen:api` regenerated in the same commit (the
P14 drift lesson).

**UI.** Restructure the existing Settings page into a **Configuration** tab
(nav rename — operator-approved): sections *Sources* (per-source cards: on/off
badge, parameters, webhook-vs-poller mode, secrets as `********`, a small
health chip client-joined from the existing doctor query — no new API),
*Storage & server*, *Normalization* (the existing rules/tag_patterns editors,
unchanged), *Retention & jobs*. Mapping-webhook templates shown in full
(operator-approved — config-as-code, same exposure class as rules). Portal
only; the embedded lite UI (`web/`) stays as-is.

**CLI.** `wtc config` subcommand rendering the same endpoint (`--json`
supported) — thin client, CLI-first parity (operator-approved: both surfaces
in scope).

**Accept:** the portal tab shows the wtc-dev rig's real config (flux HMAC on,
github/gitlab pollers off, storage postgres, ...) with zero secret values in
any `/api/v1/config` response body (sentinel guard test + a live
`curl | grep -c <known-secret>` = 0); an operator can tell at a glance which
ingest paths are live; CI green including the regenerated UI client.

## Phase 18 — Poller scope globs + Where links (operator-requested 2026-07-18)

**Shipped 2026-07-18.** As planned, with one discovery made fixture-first:
GitLab **user namespaces are not groups** — `/groups/:path/projects` 404s for
them (verified live: the rig project lives under the user `migueljfsc`), so
`ListNamespaceProjects` falls back group→user on 404 and `your-username/*`
patterns just work. Scope helpers (`SplitScope`/`ResolveScope`/
`ScopeNamespace`) live in `internal/normalize` next to the now-exported
`CompileGlob` — one glob dialect, one home. A failed discovery degrades to
the exact entries for that sweep (logged), never an aborted poll of the
pinned repos.

Two independent items; **A ships first (operator ordering)**.

### A. Glob patterns in poller repo/project scope

`sources.github.repos` / `sources.gitlab.projects` entries may carry globs —
no new config keys, exact entries keep byte-identical behavior:

```yaml
sources:
  github:
    repos:
      - my-org/*             # every repo in the org the token can see
      - my-org/my-prefix-*   # narrowed by name prefix
```

- **One glob dialect product-wide**: export the rules engine's compiler as
  `normalize.CompileGlob` (`*` = one path segment, `**` = any depth — so
  `group/*` stays flat and `group/**` reaches GitLab subgroups). Patterns
  compile at startup; a bad glob is a fatal config error, never a silent
  empty scope.
- **Resolution every sweep** (same cadence as today's empty-list
  auto-discovery, so a new repo matching a prefix is picked up without a
  restart). Per provider:
  - *github*: any glob present → `ListAccessibleRepos` (exists) → union of
    exact entries + glob matches. A bare `*/*` is allowed — it is just a
    filter over what the token can already see. Empty list stays
    "everything accessible" (unchanged).
  - *gitlab*: gains **scoped discovery** (new client method): the static
    prefix before the first glob-bearing segment is the group path; list its
    projects (`GET /groups/:path/projects`, `include_subgroups=true`,
    `archived=false`, paginated) and filter full paths against the pattern.
    A pattern with no static group prefix (`*`, `*/x`) is a config error —
    unscoped listing is exactly what P12 declined. This amends the P12 "no
    discovery" note to "no *unscoped* discovery".
- **Fixture-first**: capture a real group-projects payload from the
  wtc-demo-gitlab rig before writing the list parser; github reuses the
  existing discovery path (no new payload shape).
- Poller logs keep reporting the resolved count; the P17 config view already
  shows the patterns verbatim.

**Accept:** `my-org/*` and `my-org/prefix-*` poll exactly the matching repos
on both providers (live check on wtc-dev: `migueljfsc/wtc*` narrows the
discovered set); exact lists behave identically to today; a bad glob fails
startup; `docs/setup/github-poller.md` + `gitlab.md` document the syntax.

### B. Clickable BUILD → INTENT → APPLIED on the Where page

Every event rendered on the Where page (builds list, intents list, per-env
applied rows) and in the timeline drawer's journey links out to `event.url`
when non-empty — the exact run/PR/commit on the source system, opened in a
new tab (reuse EventDrawer's anchor idiom: `target="_blank"
rel="noreferrer"`, real anchors so keyboard/middle-click work; nested inside
clickable rows → stopPropagation). Events without a URL (flux reconciles
typically carry none) render as plain text today and stay that way — no dead
links, no invented targets. Internal deep-links (env row → filtered
Timeline) are explicitly deferred — this phase is external URLs only.

**Accept:** on the wtc-dev rig, a Where lookup of a real sha links its build
to the GitHub Actions run, its intent to the commit/PR, and an applied row
with a URL to its source; URL-less rows are visually unchanged; UI
lint/typecheck/build green.

---

# Change-intelligence & operations track (P20–P22, operator-requested 2026-07-18)

wtc records "what changed" well (ingest breadth + the three queries + portal).
This track adds the missing *dimensions*: turning the record into incident
leverage (P20), into awareness (P21), and into a record you can trust and take
with you (P22). None reopen a non-goal — the P20 scoring is a deterministic
heuristic, **not** an AI summary; no RBAC, no ClickHouse, no in-cluster agent.

**Recommended build order: P20 → P22 → P21** (value × effort × risk): P20 is
the flagship and self-contained; P22 is cheap trust-building; P21 is the only
one adding a new subsystem, so it lands last. Phase numbers follow the
operator's thematic listing, not the build order.

## Phase 20 — Incident correlation ("what changed before this broke?")

The flagship reason a change ledger exists. Alerts already ingest as anchors
(P5, `kind=alert`, env/service-inferred) and `wtc around` shows generic
time-neighbors; P20 upgrades that to a **ranked suspect list** — given an alert
(or a moment), which change most likely caused it.

- **New query `wtc blast <alert-id | RFC3339-ts> [--env --service --window 2h --json]`**.
  Anchor from an alert event (its `ts` + inferred `env`/`service`) or a bare
  timestamp. Output: candidate changes in the lookback window before the
  anchor, each with a score and a one-line why, links into `where`.
- **Deterministic score (documented, tunable-later, never ML):** recency
  (closer = higher), **same-env = hard signal**, same-service = booster (alerts
  often lack a clean service, so rank it, don't hard-filter), kind weight
  (deploy/rollback/config_change > merge/push), and a bump for a `failed`
  deploy immediately prior. Fixed weights v1, documented in the query help.
- **Forward direction too** (cheap once the engine exists): `wtc blast` on a
  *deploy* lists alerts that fired after it — "did my change cause noise?".
- **Portal:** the alert drawer's existing `AroundPanel` becomes a "Likely
  causes" ranked list (score chips, each row → Where). Endpoint checked
  2026-07-19: `/around` returns the cursor-paginated bare `EventsResponse` —
  scored suspects don't fit that shape. So: a small new `/blast` (+ `/api/v1`
  alias) sharing `handleAround`'s anchor resolution; `/around` stays untouched;
  `/blast` joins `openapi.json` (P7 drift test).
- **Anchor context:** an alert-id anchor supplies `ts`+`env`+`service` for
  scoring; a bare-ts anchor without `--env` disables the same-env signal —
  say so in the output rather than scoring blind.
- **Pure query layer — no schema change, no new deps.** Reuses events +
  inference. Table-driven tests on seeded fixtures (alert + N candidate deploys
  → asserted ranking); an E2E replay proving the alert→cause link end to end.

**Accept:** on the demo stack, an alert row surfaces the deploy that preceded
it in the same env as the top suspect, ahead of an unrelated merge; the reverse
(`wtc blast <deploy>`) lists the subsequent alert; `--json` stable; docs snippet
shows the incident-forensics workflow.

**Decisions (resolved 2026-07-19):** fixed weights v1, documented in the query
help (config-tunable deferred until someone asks); new `/blast` endpoint — the
`/around` shape doesn't fit (see Portal bullet).

## Phase 21 — Awareness (push, don't just store)

Today wtc only emits a scheduled Slack digest. P21 makes it *active*: one
subscription engine, several sink types. Largest lift of the track — a new
outbound subsystem — so it lands last.

- **Subscriptions:** config `notifications: [{ match: {env,service,repo,kind,
  status globs}, sink: {...} }]`. Emit on a **new row** OR a **dedup-upsert
  that changes `status`** — new-row-only would break status matching:
  lifecycle sources (trap #5) create the row as `pending`/`started` and reach
  `succeeded`/`failed` via upsert, so `status: failed` would never fire.
  Notification idempotency key = (event id, status): a redelivery that changes
  nothing never re-notifies. Detect the transition in the single-writer path
  (prior-status read is race-free there). Reuse the `rules[].match` glob
  dialect (`normalize.CompileGlob`) for the filter and P17 secret-masking for
  sink URLs/keys.
- **Sinks:** `slack` (reuse `internal/notify`), generic `webhook`, and
  **`grafana-annotation`** — POST deploys to Grafana's annotation API so "what
  changed" overlays dashboards (wtc already ingests *from* Grafana; this closes
  the loop; best effort-to-wow ratio).
- **Delivery:** an async worker off the single-writer path (never block ingest),
  at-least-once with bounded exponential retry, `wtc_notify_{sent,failed,dropped}`
  counters, drop-after-N logged. Do **not** feed it from the SSE broadcaster —
  that silently drops to slow subscribers and only sees new rows; use a
  dedicated bounded channel fed from the `Ingest` funnel (where the
  deduped/status-transition facts already live), queue-full increments the
  dropped counter. Best-effort in-memory queue v1; a durable outbox table
  (survives restart, no double/drop) is the stretch.
- **Optional bonus:** a read-only Atom/RSS feed (`/feed?env=prod`) for
  pull-based subscribers — cheap, neutral, no auth-state.
- **Fixture-first:** capture a real Grafana annotation API round-trip against a
  local Grafana before writing that sink (same discipline as the P14 preset).

**Accept:** a `notifications` entry matching `env: prod` fires a Slack message
and a Grafana annotation on the next prod deploy; a `status: failed` entry
fires when a deploy's row upserts to `failed` — once, even if the failed
payload is redelivered; a re-ingested (deduped, same-status) event does
**not** re-notify; a failing sink retries then drops with a moved counter,
never blocking ingest; sink secrets masked in `wtc config`; `docs/setup`
snippet per sink.

**Decisions:** durable outbox vs best-effort queue v1 (rec: best-effort +
retry, outbox stretch); ship B1 slack/webhook then B2 grafana as sub-phases
(rec: yes); include the Atom feed now or defer (rec: include if cheap).

## Phase 22 — Harden the record (trust / compliance)

Make wtc trustworthy *as* a system-of-record. Export/backup are read-side;
`explain` needs one nullable ingest-time column (below). Still self-contained
and low risk — good to slot second.

- **`wtc export --env --service --since --until --format csv|json|ndjson`**
  (+ streaming `/api/export`): "every prod change in Q3." Audit/compliance is a
  real selling angle and there is no export today. NDJSON streams large ranges
  without buffering.
- **`wtc backup <path>`:** WAL-safe consistent SQLite snapshot taken while
  serving — the single-binary durability story. Transport respects the process
  model (the CLI never opens the DB, and the server may be remote): `GET
  /api/backup` runs `VACUUM INTO` a server-side temp file (fine on the read
  pool under WAL), streams it to the client, deletes the temp; `wtc backup
  <path>` writes the stream locally. Plus a `docs/setup/backup.md` recipe
  (cron → object store; litestream for continuous replication). Postgres
  backend: `/api/backup` returns a clear "use pg_dump" error; docs cover
  managed/`pg_dump` backups (out of wtc's hands).
- **`wtc explain <event-id>`:** report *which rule* set each of
  env/service/cluster/kind (first-writer-wins trace) — trust in the product's
  hardest problem. Verified 2026-07-19: the engine matches ingest-time
  `normalize.Facts`, which are never persisted and are NOT reconstructible
  from stored payloads (the github poller synthesizes `Payload` via
  enrichment; flux stores message+revision only; webhook headers aren't kept).
  So: add a nullable `facts` JSON column (redacted, per-dialect append-only
  migration), populated at ingest; `explain` re-runs the **current** engine
  (incl. P17 DB-override rules — "what would happen now" is the useful
  semantics; say when the trace may differ from ingest-time) over stored facts
  via a trace-collecting `Engine.Explain`, reporting per field: rule index +
  matched pattern, "set by normalizer" (parser pre-filled, rules never ran),
  or "unmatched". Rows ingested before the column get a clear "facts not
  recorded" message, never a guess.

**Accept:** `wtc export` round-trips a known window to CSV/NDJSON with correct
filters and stable column order; `wtc backup` against a live (remote) server
produces a snapshot that opens as a valid DB with identical `log` output while
the server keeps serving; `wtc explain` names the matching rule for a demo
event's env, and reports "facts not recorded" for a pre-migration row; docs
cover the backup cron/litestream recipe.

**Decisions:** built-in `wtc backup` + litestream docs vs docs-only (rec: both);
export formats (rec: CSV + NDJSON, JSON array optional); facts column vs a
zero-schema `wtc explain --facts '{...}'` rules debugger (rec: the column — it
keeps the "explain THIS event" promise; **needs operator sign-off**, it drops
the track's original no-schema-change claim).

### Sequencing (P20–P22)

Build **P20 → P22 → P21**. P20 is pure read-side; P22 is read-side plus one
nullable ingest-time `facts` column — and the earlier that lands the better,
since facts are captured only for rows ingested after the migration, so
`explain` coverage grows with time; P21 introduces the outbound worker/sinks
and is the only one that can affect the ingest path, so it goes last once the
query surface is settled. Each phase keeps the DoD: fixtures + tests + a
`docs/setup` snippet wiring it to real infrastructure + CHANGELOG.
