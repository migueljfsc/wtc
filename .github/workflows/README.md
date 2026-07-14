# Workflows

| File | What it does |
|---|---|
| `ci.yml` | wtc itself: build + vet + test + golangci-lint on every push/PR; on main additionally publishes `ghcr.io/migueljfsc/wtc:{sha-<sha7>,latest}` |
| `release.yml` | **manual** (`workflow_dispatch`): commitizen bumps the wtc version (tag `vX.Y.Z`, updates `.cz.yaml` + Helm `appVersion`), then pushes the versioned image `ghcr.io/migueljfsc/wtc:{X.Y.Z,vX.Y.Z,latest}`. Optional `increment` input forces PATCH/MINOR/MAJOR. |
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

## Why wtc releases are manual (release.yml), demos are auto

This is a monorepo: `feat(demo-api)` / `bump(demo-web)` commits share main's
history, and commitizen computes its increment from *all* commits since the
last tag — it can't scope by path. Auto-bumping wtc on every merge would
inflate its version on demo noise (a dry-run today jumps 0.1.0→0.2.0 purely
from demo feats). So wtc releases are cut deliberately via
`release.yml`; the demo services, whose whole job is generating events,
auto-bump. Root tags (`vX.Y.Z`) and demo tags (`demo-<svc>-vX.Y.Z`) don't
collide, so the two commitizen configs coexist.
