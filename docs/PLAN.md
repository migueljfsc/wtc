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
| **P8 portal core views** | ⬜ | dashboard, rich timeline, service pages |
| **P9 change-intelligence views** | ⬜ | `where`/`diff`/`around` visualized; env matrix |
| **P10 live + config surfaces** | ⬜ | SSE live updates, rules/sources settings UI, deploy path |

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
