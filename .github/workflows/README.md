# Workflows

| File | What it does |
|---|---|
| `ci.yml` | Checks only, on every push/PR: `test` (go build + vet + test), `lint` (golangci-lint), `ui` (npm ci + client-drift check + lint + typecheck + build), `commits` (commitizen conventional-commit check over the PR range, PR-only). No image builds or version bumps here. |
| `build-publish.yml` | wtc binary image. On main push (independent of `ci`, like the demo publishers) + `workflow_dispatch`: commitizen-bumps the wtc version when wtc's own code changed (tag `vX.Y.Z`, updates root `.cz.yaml` + Helm `appVersion`), then pushes `ghcr.io/migueljfsc/wtc:{sha-<sha7>,X.Y.Z,vX.Y.Z,latest}` (semver tags only on a bump). |
| `build-publish-ui.yml` | Portal image, with its own cz lifecycle (`ui/.cz.yaml`, tags `wtc-ui-vX.Y.Z`, bumps `ui/package.json`), independent of the wtc version — mirrors the demo services. Push touching `ui/**` (a `paths` filter scopes the bump): cz bumps the ui version ([skip ci]), pushes `ghcr.io/migueljfsc/wtc-ui:{sha-<sha7>,X.Y.Z,vX.Y.Z,latest}`. `workflow_dispatch` builds without bumping. |
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

## Monorepo + commitizen

wtc (root `.cz.yaml`, tags `vX.Y.Z`), the portal (`ui/.cz.yaml`, tags
`wtc-ui-vX.Y.Z`), and each demo service (`demo/<svc>/.cz.yaml`, tags
`demo-<svc>-vX.Y.Z`) each auto-bump on merge with their own independent
version. The tag formats don't collide, so the configs coexist. Because cz
can't scope commits by path, each publisher confines its own bump: the wtc bump
in `build-publish.yml` uses a `git diff` guard (skips demo-/docs-/ui-only
pushes), while `build-publish-ui.yml` and the demo workflows use `paths`
filters on their triggers. Each increment is still computed from all commits
since that lifecycle's last tag, so unrelated `feat`s can nudge it. All configs
need the `CZ_TOKEN` secret and the checkout-credential fix above; `build-publish`
and `build-publish-ui` share the `release` concurrency group so their bump
pushes to main serialize.
