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

## Gaps (deliberate)

- **GitHub webhook envelopes** — skipped by operator decision (no public
  endpoint; poller is primary). `/ingest/github` authenticates + captures
  only, until webhook fixtures exist.
- **Flux ImageUpdateAutomation events** — need a cluster with image
  automation writing to a repo; capture when the real dev cluster exists.

Golden tests live next to their normalizers
(`internal/ingest/github/normalize_test.go`, `internal/ingest/flux/flux_test.go`).
