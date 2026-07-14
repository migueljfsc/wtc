# Workflows

| File | What it does |
|---|---|
| `ci.yml` | wtc itself: build + vet + test + golangci-lint on every push/PR; on main additionally publishes `ghcr.io/migueljfsc/wtc:{sha-<sha7>,latest}` |
| `demo-api.yml` / `demo-web.yml` / `demo-worker.yml` | one per dummy service (split so each `workflow_run` event attributes to a service in wtc). Push touching `demo/<svc>/**` (manifests excluded): commitizen bumps `demo-<svc>-vX.Y.Z` and pushes the version commit, image lands in GHCR tagged `sha-<sha7>` + `<version>-<sha7>`. Staggered crons + a coin flip generate background events. |

## Demo-pipeline requirements (learned the hard way)

- **Repo secret `CZ_TOKEN`** — a fine-grained PAT (this repo only,
  Contents: read/write). The bump commit must push to `main`, which the
  ruleset restricts to PRs; `github-actions[bot]` can't bypass it and the
  Actions app can't be added to the bypass list on user-owned repos, so the
  push must authenticate as the repo owner.
- **The PAT must be the `checkout` token**, not just commitizen's
  `github_token`: actions/checkout persists its credential as an
  `http.extraheader` in `.git/config`, which silently overrides any token in
  a push URL. Three GH013 rejections were diagnosed before this stuck.
- Bump commits carry `[skip ci]`; a shared `concurrency: demo-release` group
  serializes pushes across the three workflows.
