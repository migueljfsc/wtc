## demo-worker-v0.4.0 (2026-09-06)

### Feat

- cluster facet + Argo-per-env instance identity
- DORA lead time per env (build/merge → deploy)
- **ui**: Services page honors the global scope; its selection moves to ?svc=
- **ui**: custom time range in the scope bar, re-enabling point-in-time views
- scope facets narrow dashboard, changes & diff (phase 2)
- **ui**: global scope bar (phase 1) — Timeline/Dashboard/Changes
- **query**: search also matches ref/env/repo/owner, not just text fields
- **ui**: whole Changes row → timeline scoped to that exact change
- **query**: changesets — collapse build→deploy per commit
- **query**: DORA change-failure rate & MTTR by env and team
- **catalog**: infer owning team from a service catalog
- **query**: point-in-time state via `--at` on diff and matrix
- awareness — notifications, sinks, Atom feed (P21)
- harden the record — wtc export, backup, explain (P22)
- wtc blast incident correlation (P20)
- repo dimension + timeline facet for monorepos
- multi-select timeline facets + new source facet
- **ui**: link timeline drawer ref to Where; plan P20–P22
- Flux/ArgoCD ingest scope (allow/deny by raw facts)
- **ui**: searchable service list, wider pages, capped filter comboboxes
- configurable first-poll backfill window + multi-cluster hub docs
- poller scope globs + clickable Where journey (P18)
- **ui**: source brand marks on timeline rows
- **ui**: pulse the started status dot
- settings tab — versions, connection, browser preferences
- configuration visibility — portal tab + wtc config (P17)
- prometheus metrics (P16)
- postgres backend — stateless wtc pod (P15)
- mapping webhook — config-declared long-tail ingest (P14)
- github webhook completion — poller/webhook peer modes (P13)
- gitlab ingest — SCM/CI-axis neutrality peer of GitHub (P12)
- argocd ingest — second GitOps engine alongside Flux (P11)
- poller repo auto-discovery + helm secretRef env injection
- **helm**: deploy the portal by default + optional same-origin ingress
- **p10**: editable rules with hot-reload — completes P10
- **p10**: SSE live updates + read-only config surfaces
- **p9**: service detail + alert correlation — completes P9
- **p9**: env matrix — where + diff visualized
- **p8**: timeline, event drawer, facets + actor filter
- **p8**: dashboard + stats aggregation endpoints
- **p7**: portal foundation — SPA scaffold + API hardening
- **p6**: finish release hygiene — retention, demo seed, goreleaser, load sanity

### Fix

- custom range end, argocd repo facet, non-ASCII globs, export filters (#16)
- **ui**: change drill-in is a precise ref filter, shown as a scope-bar chip
- **ui**: Changes row opens timeline via ?q sha-search, reflected in the scope bar
- **ui**: scope bar — filters left, time range right (Grafana-style)
- **ui**: As-of field — icon visible in dark mode, tighter layout
- **ui**: Changes nav before Timeline; larger, obvious As-of date field
- point-in-time matrix on postgres + clickable Changes cards
- **ui**: order timeline filters repo before service
- **ui**: flush-left facet options, trailing check only when selected
- **ui**: size facet dropdowns to content; raise Clear/Save to the header
- don't link non-git-traceable diff cells; soften Where for OCI digests
- empty API lists marshal as [], never null — portal crash
- **ui**: floor deploy-frequency window at one week
- **ui**: render UI version with the v-prefix the API stamp carries
- regenerate ui api schema for P14 webhook_mapping_errors drift
- **ui**: diff legend — color 'drift' amber and break onto separate lines
- **ui**: diff matrix highlights the env behind the newest deploy

### Refactor

- drop unused Mapper.Name and PresetNames

## demo-worker-v0.3.0 (2026-07-14)

### Feat

- phase 5 slack digest + docs — surfaces complete
- phase 5 — embedded web timeline + alertmanager ingest + wtc around
- phase 4 packaging — Dockerfile, Helm chart, compose, CI publish
- **wrap**: wtc wrap with helm/terraform sniffers + rollback heuristic

### Fix

- **docker**: multi-arch images (linux/amd64 + linux/arm64)
- **config**: don't treat ${VAR} inside YAML comments as live references
- **docker**: keep web/ in build context — go:embed needs it

## demo-worker-v0.2.0 (2026-07-14)

### Feat

- **demo-worker**: add healthz endpoint
- **demo-api**: add healthz endpoint
- workflow fact for monorepo build attribution + dev overlay pins
- **demo-web**: trigger first demo build
- **query**: wtc where / diff / handoff — the three killer queries
- **github**: PR-diff enrichment — paths facts + image-bump extraction
- **query**: FTS5 search + tag_patterns resolver — phase 3 foundation
- **demo**: dummy-service test bed — 3 services, cz lifecycles, flux wiring
- **flux**: phase 2 — notification-controller ingest, fixture-first
- wtc doctor + wtc init + github setup docs — phase 1 complete
- **github**: normalizers + rules engine, poller ingests end-to-end
- **github**: phase 1 step 0 — capture mode, HMAC webhooks, poller skeleton
- phase 0 skeleton — store, serve, record, log end-to-end

### Fix

- **demo**: pass CZ_TOKEN to checkout — extraheader overrides push URL auth
- **demo**: reconcile cz versions with orphaned tags, drop token diagnostic
- **demo**: cz bump pushes authenticate via CZ_TOKEN PAT
- apply post-phase-0 review — 22 confirmed findings
