# wtc — vendor-neutral change ledger ("git log for production")

> Working name `wtc` ("what the change" / "what changed"). Rename is a find-replace; do not bikeshed it mid-build.

## Mission

A single self-hosted binary that ingests change events (CI builds, GitOps reconciles, helm/terraform runs, manual changes) from heterogeneous sources, normalizes them into one schema, and answers three questions fast:

1. **What changed?** — `wtc log --env prod --since 2h`
2. **Where is this commit?** — `wtc where <sha>` (build → PR merged → reconciled per env)
3. **How do two environments differ right now?** — `wtc diff staging prod`

Differentiator: vendor-neutral, self-hosted, CLI-first. New Relic, Datadog, and Harness sell change tracking locked inside their platforms. Nothing neutral, standalone, and open exists. That neutrality is the product; never add a hard dependency on any single vendor's ecosystem.

## Operator context (the first user — build for this stack first)

- CI: GitHub Actions. Terraform runs in CI only (never locally).
- Deploys:
  - Feature branches: manual/scripted `helm install` into ephemeral envs (e.g. `pr-123`).
  - dev: Flux image automation auto-commits new image tags; Flux reconciles.
  - staging/prod: a human opens a PR bumping the image tag; merge → Flux reconciles.
- Repo layout: **microservices** — one repo per service; each repo carries its deploy/infra manifests under `./infrastructure` (path configurable, `infra_path`), kustomize-style: `infrastructure/base/**` + `infrastructure/overlays/<env>/**`. No central manifests monorepo.
- Image tags **embed the git sha**: `sha-<shortsha>` and `<semver>-<sha>` both occur. wtc ships a configurable `tag_patterns` list (these two as defaults) — never hardcode one convention.
- Clusters: **cluster-per-env** — `dev`, `staging`, `prod`. Default mapping: cluster name = env name. Kubernetes on AWS and Hetzner. Flux v2.x everywhere (notification-controller with Provider/Alert CRDs, generic-hmac, image-automation on dev).
- Network: wtc deploys **inside the org's private environment** (EKS or similar); no public endpoint in v1. GitHub webhooks cannot reach it → the **GitHub API poller is the primary GitHub ingest path**. Webhook handlers are still built (HMAC and all) for deployments that do have a public endpoint. Flux notification traffic is in-cluster and unaffected.
- IaC: mostly YAML manifests managed by Flux, including Crossplane resources. Occasional Terraform (CI only).
- Packaging: **Helm chart first** (in-cluster), docker-compose for VMs/local. No systemd unit in v1.

Consequence: the two highest-value ingest paths are the **GitHub API poller** and **Flux notification-controller**. Crossplane changes are covered indirectly (they flow through git + Flux). Helm-for-feature-branches and Terraform are covered by the `wtc wrap` command until later phases.

## Hard decisions — do not relitigate without operator approval

- **Language:** Go >= 1.22. Single static binary. **No CGO** — use `modernc.org/sqlite`.
- **Storage:** SQLite, WAL mode, single writer (the serve process). Migrations are embedded sequential SQL files via `go:embed`, append-only, never edited after being applied.
- **Process model:** one binary, subcommands. `wtc serve` is the daemon (ingest HTTP + query API + retention job). Every other subcommand is a thin HTTP client of the serve API. **The CLI never opens the DB file directly.**
- **CLI framework:** `spf13/cobra`. Config: single YAML file + `WTC_*` env overrides. No viper; use koanf or hand-rolled loading.
- **IDs:** ULID. **Time:** stored UTC RFC3339; `ts` = source event time, `ingested_at` = ours; timelines sort by `ts`; CLI renders local time.
- **Ingestion is at-least-once.** Idempotency via a `dedup_key` UNIQUE index + upsert. Every normalizer MUST derive a stable `dedup_key` from source-side identifiers (run id, delivery id, object+revision+reason) — never from received-at time. This is what makes webhook loss recoverable by a later sweeper/poller.
- **Auth:** per-source HMAC on webhook paths (GitHub `X-Hub-Signature-256`; Flux `generic-hmac` provider). Static bearer tokens for `/ingest/generic` and all `/api/*`. No users, no RBAC in v1.
- **Redaction before storage:** raw payloads pass a regex deny-list (AWS keys, `ghp_`/`github_pat_` tokens, bearer tokens, `password|secret|token[:=]` values). Terraform plan bodies are never stored — summary counts only.
- **Web UI (phase 5 only):** toolchain-free. Hand-written HTML/CSS/vanilla JS (htmx allowed), embedded with `go:embed`. **No node, no npm, no bundler.** If a task seems to need React, the task is out of scope.
- **License:** Apache-2.0.

## Non-goals for v1 — do not build

Postgres backend, multi-tenancy/RBAC, DORA dashboards, feature-flag providers, in-cluster Kubernetes agent, Slack slash-commands, AI summaries, any SPA framework.

## Repository layout

```
cmd/wtc/              main.go + cobra command definitions
internal/model/       Event struct, kind/status enums, validation
internal/store/       sqlite open/pragmas, migrations/, write queue, queries
internal/server/      http server, routing, middleware (hmac, bearer, ratelimit), capture mode
internal/ingest/
    github/           payload structs, handlers per event type, normalizer
    flux/             eventv1 payload, dedup/suppression, normalizer
    generic/          /ingest/generic + `wtc record` schema
internal/normalize/   rules engine (env/service/cluster inference), redaction
internal/query/       log, where, diff, handoff, doctor logic
internal/wrap/        `wtc wrap` command runner + helm/terraform arg sniffers
web/                  phase 5 embedded UI
testdata/             captured real payloads as golden fixtures, per source
demo/                 dummy services (api/web/worker) that feed wtc real events:
                      own go modules (invisible to root build), commitizen
                      lifecycles, kustomize overlays, flux/ cluster wiring
docs/                 SPEC.md, PLAN.md, setup/ (flux-provider.yaml, github-webhook.md)
```

## Engineering conventions

- **Fixture-first development.** `wtc serve --capture-dir ./testdata/raw` dumps every raw ingest body (+headers) to disk. Workflow for every new source/event type: wire the real source → capture real payloads → freeze curated fixtures under `testdata/<source>/` → write the normalizer test against fixtures → then implement. No normalizer merges without golden-fixture tests (`fixture.json` → expected normalized `Event`).
- Table-driven tests. `go test ./...` and `golangci-lint run` must pass before any phase is called done.
- E2E smoke: `httptest` replays fixtures through the full server into a temp DB, then asserts `log`/`where`/`diff` outputs.
- No panics on server paths; wrap errors with `%w` and context.
- Every query subcommand supports `--json`.
- Minimal dependencies; justify each new module in its commit message. Conventional commits.
- Single write goroutine consumes an ingest channel; SQLite opened with `_journal_mode=WAL`, `_busy_timeout=5000`; separate read-only pool for queries.

## Known traps — respect these while implementing

1. **Flux event spam.** notification-controller re-emits on every reconcile. Dedup on `(object kind/ns/name, revision, reason)`; drop repeats within a suppression window (default 10m, configurable). Without this the timeline is unusable.
2. **Env/service inference is the product's hard problem.** Never trust payload fields directly; every event passes through the ordered rules engine (see SPEC). Unmatched events get `env=""` and are surfaced by `wtc doctor` — never guess silently.
3. **GitHub push payloads truncate file lists** on large pushes. Path-based env inference must treat a truncated list as "unknown", not "no match". Accurate backfill via the compare API comes with the phase-4 sweeper (requires an API token).
4. **Webhook loss.** Not the operator's problem in v1 (the poller is the primary path), but for webhook-mode deployments: stable dedup keys (done by design), GitHub redelivery, and the poller doubling as a sweeper that re-ingests idempotently.
5. **status lifecycle.** `workflow_run` fires requested → in_progress → completed for the same run id. Model: ONE row per logical change; upsert on dedup_key updates `status`, `ts`, `duration_ms`. Do not create a row per transition.
6. **Out-of-order arrival and clock skew.** Sort by source `ts`; keep `ingested_at`; if `|ts - ingested_at|` > 10m, flag in doctor output.
7. **Ephemeral env cardinality** (`pr-123`, `pr-124`, …). Allowed; retention plus an `env LIKE 'pr-%'` archival rule keeps them from polluting `diff`/`handoff` defaults.
8. **The tag↔sha join** powers `wtc where`. Operator's tags embed the git sha (`sha-<shortsha>`, `<semver>-<sha>`), so the join works out of the box via the configurable `tag_patterns` list. Build events should still carry produced tags in `artifacts[]` when available — patterns are the general mechanism, not an excuse to skip explicit artifact reporting.
9. **Flux payload shape.** Treat the notification event structure (`involvedObject`, `reason`, `message`, `metadata` incl. revision key) as unverified until real fixtures are captured from the operator's cluster. Build parsers against captured fixtures, not documentation memory.

## Definition of done, per phase (see docs/PLAN.md)

Code + tests + fixtures + a docs/setup snippet showing how to wire the real source + CHANGELOG entry. A phase is not done if the operator cannot wire it to real infrastructure using only the docs.

## Make targets

`make build` · `make test` · `make lint` · `make run` (serve with `./dev/wtc.yaml`) · `make fixtures` (golden tests only)
