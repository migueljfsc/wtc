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
| **P16 Prometheus metrics** | ⬜ planned | `/metrics` (promhttp): per-source ingest/dedup/suppression counters, mapping errors, poller lag, DB size, HTTP latency histograms; optional ServiceMonitor in the chart. ClickHouse evaluated and rejected — change-event volumes never warrant it |

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
