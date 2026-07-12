# wtc — Build Plan

Lives at `docs/PLAN.md`. Each phase ≈ 1–3 Claude Code sessions. A phase is done only when its acceptance criteria pass and the operator can wire it to real infrastructure using only `docs/setup/`.

## Open questions (answers gate the marked phases)

| # | Question | Gates |
|---|---|---|
| Q1 | Image tag convention: do tags embed the git sha (`app:sha-abc1234` / `app:abc1234`), or semver/branch/timestamp? If sha not embedded: is adding one reporting step to CI acceptable? | P3 `where` design |
| Q2 | Cluster topology: cluster-per-env or shared cluster with env namespaces? How many clusters total across AWS + Hetzner, and do all run Flux? | P2 provider setup, `cluster` fact |
| Q3 | Network: can wtc serve get a public HTTPS endpoint reachable by GitHub (Hetzner VM or ingress + TLS)? If not, the GitHub poller moves from P4 into P1 as the primary ingest path. | P1 |
| Q4 | Manifests repo layout: single Flux monorepo (`clusters/<env>/...`? apps/overlays?) or per-app repos. Exact path conventions → shipped default rules. | P1/P2 rules |
| Q5 | Terraform runs: locally by humans, in CI, or both? | P4 scope |
| Q6 | Flux version (v2.x assumed) — notification-controller with Provider/Alert CRDs available, `generic-hmac` supported, image-automation in use for dev? | P2 |
| Q7 | Where will wtc itself run: systemd on a VM or in-cluster? Decides which packaging lands first in P4. | P4 |

Defaults if unanswered: assume sha-embedded tags are NOT guaranteed (build the generic report step), cluster-per-env, public endpoint available, monorepo `clusters/<env>/`, terraform local, Flux v2 current, systemd-on-VM first.

## Phase 0 — Skeleton (foundation)

Repo init, Go module, Makefile, CI (GH Actions: test+lint+build). Cobra skeleton with `serve`, `record`, `log`, `init`, `doctor` stubs. Config loader + `${VAR}` expansion + env overrides. SQLite store: pragmas, embedded migrations, single-writer goroutine + ingest channel, read pool. `POST /ingest/generic`, `GET /healthz`, `GET /api/events` (basic filters). `wtc record` and `wtc log` end-to-end.

**Accept:** `wtc serve` + `wtc record --kind manual --env dev --service api --title test` + `wtc log --since 1h` round-trips; duplicate record with same dedup_key does not duplicate; `go test ./...` green including store tests on temp DB.

## Phase 1 — GitHub ingest

HMAC middleware. Capture mode (`--capture-dir`). Parsers + normalizers for `workflow_run`, `push`, `pull_request` (closed+merged). Rules engine per SPEC §3 with fact map (repo, branch, event, paths, actor…). Status-upsert lifecycle for workflow_run. Redaction pass. `doctor` counts real data.

**Accept:** golden-fixture tests for ≥6 captured real payloads (build started/success/failure, push to manifests by human and by flux bot, merged PR); truncated-paths case lands `env=""` not misrouted; live: operator's repos wired, events visible in `wtc log` within seconds; replaying the same delivery twice yields one row.

## Phase 2 — Flux ingest

`/ingest/flux` with generic-hmac verification. Parse notification events from **captured fixtures first** (do not trust remembered field names). Dedup `(object, revision, reason)` + suppression window. Map severity → status. Cluster identification per Q2 (Alert metadata or per-cluster ingest path — decide from fixtures). Rules mapping Kustomization/HelmRelease/ImageUpdateAutomation → service/env. Ship `docs/setup/flux-provider.yaml`.

**Accept:** fixtures for reconcile success, reconcile failure, image-automation commit; a reconcile loop firing N identical events stores 1 row; dev-env flow visible end-to-end in `wtc log --env dev`: flux-bot push (github) → reconcile (flux); prod flow: human PR merge → reconcile, both rows carrying the same manifest revision.

## Phase 3 — The three killer queries

`where` (BUILD → INTENT → APPLIED per SPEC §6, honoring Q1 answer), `diff`, `handoff`, FTS5 `-q`. GitHub PR-diff enrichment (SPEC §7) if `api_token` present — this is what links tag bumps to manifest revisions. Seeded-fixture timeline for deterministic query tests.

**Accept:** on a seeded timeline reproducing the operator's real flow (build → PR bump → merge → reconcile in staging, later prod), `wtc where <sha>` shows both envs with correct intent→applied lag; `wtc diff staging prod` flags the not-yet-promoted service; `handoff` renders correct counts; every query has `--json` golden tests.

## Phase 4 — Gap closers

`wtc wrap` with helm/terraform sniffers (SPEC §5) covering feature-branch installs and terraform (scope per Q5). GitHub sweeper: poll recent workflow runs + merged PRs for configured repos, re-ingest idempotently (fills webhook-loss gaps; becomes primary path if Q3 = no public endpoint). Revert-PR heuristic → kind=rollback. `docs/setup/gha-report-step.md` (needed if Q1 = tags don't embed sha). Packaging per Q7: systemd unit + Dockerfile (+ kustomize overlay).

**Accept:** `wtc wrap -- helm upgrade pr-123 ./chart -n pr-123` records started→succeeded with service/namespace inferred; killing serve, generating GitHub activity, restarting, and running the sweeper backfills with zero duplicates; wrap with server down still runs the wrapped command and exits with its code.

## Phase 5 — Surfaces

Embedded web timeline (toolchain-free per CLAUDE.md): filter bar (env/service/kind/text), day-grouped stream, event → source deep link. Alertmanager ingest + `wtc around <ts|alert>` (changes in ±window before an alert). Slack digest: `wtc handoff --slack-webhook <url>` + optional cron in serve.

**Accept:** UI served from the single binary, usable on mobile width, no external asset downloads; alert fixture correlates to the deploy that preceded it; digest posts to a real Slack webhook.

## Phase 6 — Release hygiene

goreleaser (linux/darwin, amd64/arm64), versioned migrations check, README with 5-minute quickstart, demo seed command (`wtc demo` loads a synthetic week), retention job verified, basic load sanity (10k events: log/diff < 100ms).

## Sequencing notes

- P0→P1→P2 strictly ordered. P3 needs P1+P2 data shapes. P4 parallelizable after P1. P5 last.
- Capture fixtures the moment real sources are wired (P1/P2) — they are the contract for everything downstream.
- If Q3 answer removes the public endpoint, re-order: sweeper (from P4) implements before P1 webhooks finish; HMAC middleware still built (Flux traffic is in-cluster/private anyway).
