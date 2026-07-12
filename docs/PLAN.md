# wtc — Build Plan

Lives at `docs/PLAN.md`. Each phase ≈ 1–3 Claude Code sessions. A phase is done only when its acceptance criteria pass and the operator can wire it to real infrastructure using only `docs/setup/`.

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

Fixture strategy without a public endpoint: capture poller API responses live; webhook payload fixtures via GitHub's hook-deliveries API or a temporary tunnel session.

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

## Phase 6 — Release hygiene

goreleaser (linux/darwin, amd64/arm64), versioned migrations check, README with 5-minute quickstart, demo seed command (`wtc demo` loads a synthetic week), retention job verified, basic load sanity (10k events: log/diff < 100ms).

## Sequencing notes

- P0→P1→P2 strictly ordered. P3 needs P1+P2 data shapes. P4 parallelizable after P1. P5 last.
- Capture fixtures the moment real sources are wired (P1/P2) — they are the contract for everything downstream.
- Q3 re-order applied: the GitHub poller (formerly P4 sweeper) is now in P1 as the primary ingest path; HMAC middleware still built in P1 (webhook mode stays supported; Flux traffic is in-cluster/private anyway).
