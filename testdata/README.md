# testdata/ — frozen real payloads

**Fixture-first rule** (CLAUDE.md): no normalizer exists without a golden
test against a real captured payload. These files are the contract between
the outside world and `internal/ingest/*` — never hand-edit one to make a
test pass; re-capture instead.

## How they were captured

`wtc serve --capture-dir ./testdata/raw` dumps every raw ingest body +
headers (`raw/` is gitignored). Fixtures here are curated copies:

- `github/rest/` — poller API responses from the real repos
  (migueljfsc/{wtc,motorcycle-journey,...}), July 2026. Covers the full
  workflow_run lifecycle (queued/in_progress/completed × success/failure —
  the queued/in_progress pair captured by re-running a live build under a
  5s poll), a merged PR, a PR file list, a default-branch commit, and empty
  responses.
- `flux/` — notification-controller v1beta3 deliveries from a kind cluster
  running Flux v2.9 (podinfo reconciles): Kustomization success + failure,
  HelmRelease install. These pinned the real `X-Signature: sha256=<hex>`
  format and the `metadata.revision` shape (`master@sha1:<sha>`).
- `argocd/` — notifications-controller webhook deliveries from Argo CD
  v3.4.5 (kind cluster, `argocd` namespace) driven by
  `docs/setup/argocd-notifications.yaml` against three guestbook
  (argoproj/argocd-example-apps) Applications wired to exercise each tier of
  the env-inference matrix: `sync_succeeded.json`/`sync_running.json`
  (`wtc-guestbook-labeled`, `labels.env=staging`), `sync_failed.json`
  (`wtc-guestbook-ns`, path pointed at a nonexistent dir — Argo calls this
  `operationPhase: Error`, not `Failed`, when manifest generation itself
  can't run), `health_degraded.json` (`demo-api-staging`, live image patched
  to a nonexistent tag — health degrades independent of any new sync;
  `operationPhase` stays `Succeeded`, the last real one),
  `sync_succeeded_env_from_namespace.json` (`wtc-guestbook-ns`,
  `destNamespace: staging`, no label) and
  `sync_succeeded_env_from_name_suffix.json` (`demo-api-staging`,
  `destNamespace: prod` — neither label nor namespace signal "staging", only
  the app name suffix does). `envLabel` is a field wtc's canonical template
  adds beyond the originally-specified list (app/project/revision/.../
  triggeredBy) — no field in that list carries Application labels, and the
  "env app label" inference tier is untestable without it; flagged for
  operator confirmation before the parser stage.

## Gaps (deliberate)

- **GitHub webhook envelopes** — skipped by operator decision (no public
  endpoint; poller is primary). `/ingest/github` authenticates + captures
  only, until webhook fixtures exist.
- **Flux ImageUpdateAutomation events** — need a cluster with image
  automation writing to a repo; capture when the real dev cluster exists.
- **ArgoCD multi-source apps (`revisions[]`, 2.6+)** — all three test
  Applications are single-source; `revisions` is captured as `null` in every
  fixture. Needs an Application with `spec.sources` (plural) to pin the
  array shape.
- **ArgoCD `triggeredBy`** — `null` in every captured fixture. Every sync in
  this batch was triggered by patching `Application.operation` directly via
  `kubectl` (cluster-admin, no Argo user session), which never populates
  `operation.initiatedBy.username`. Needs a sync triggered through an
  authenticated `argocd` CLI/UI session (or Argo's own auto-sync) to pin the
  populated shape.
- **ArgoCD parser/normalizer** — this stage (P11 stage 1) is capture-only;
  `/ingest/argocd` authenticates (`X-WTC-Token`, constant-time compare) and
  captures, same posture as `/ingest/github` before its fixtures existed.

Golden tests live next to their normalizers
(`internal/ingest/github/normalize_test.go`, `internal/ingest/flux/flux_test.go`).
ArgoCD's normalizer/golden tests land in a later stage
(`internal/ingest/argocd/`, not yet created).
