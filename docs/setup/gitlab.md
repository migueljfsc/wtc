# Wiring GitLab (poller + webhook)

GitLab is the SCM/CI-axis neutrality peer of GitHub: same normalized Events,
same `where`/`diff`/`handoff` behaviour, no GitHub dependency. Two ingest
modes, both first-class and safe to run together (they converge on the same
dedup keys):

- **API poller** — needs only **outbound** HTTPS to your GitLab. Primary path
  when wtc has no public endpoint. Pulls pipelines, merged MRs, and
  default-branch commits; idempotent, so it also heals anything a webhook
  deployment missed.
- **`/ingest/gitlab` webhook** — push-latency ingest when wtc is reachable.

Works against gitlab.com and self-managed GitLab (`base_url`).

## 1. Access token

A **project**, **group**, or **personal** access token with:

- `api` scope (or `read_api`) — pipelines, MRs, commits, and MR *changes*.
- `read_repository` is implied by `api`; not needed separately for ingest.

Read-only is enough. A project access token scoped to each watched project is
the least-privilege choice. Rate budget is generous; one project costs ~4
requests per poll (three lists + one detail per new pipeline).

## 2. Poller config

```yaml
sources:
  gitlab:
    base_url: https://gitlab.com          # set to your instance for self-managed
    api_token: ${WTC_GITLAB_API_TOKEN}    # PRIVATE-TOKEN; enables poller + MR-diff enrichment
    poll_interval: 60s                    # 0 disables the poller (webhook-only)
    projects:
      - your-group/app-api                # exact
      - your-group/svc-*                  # glob: that group's matching projects
      - your-group/**                     # glob: subgroups too
    infra_path: infrastructure/           # per-project manifests dir
```

`projects` entries may be **globs** (`*` = one path segment, `**` = any depth
— the same dialect as `rules:` matches), resolved every sweep so new projects
matching a pattern are picked up without a restart. A glob's **static prefix
is mandatory** (`your-group/*`, never a bare `*`): the prefix names the group
or user namespace whose project list is fetched — that scoping is what keeps
discovery bounded, and unscoped patterns fail at startup. User namespaces
work too (`your-username/*`); wtc falls back from the group endpoint to the
user endpoint automatically.

Export `WTC_GITLAB_API_TOKEN` in the serve environment (Kubernetes: a Secret →
env var). Never write the token into the file. Unlike the GitHub poller there
is still no "watch everything the token can see" mode — every glob is scoped
to a namespace.

### Verify

```bash
wtc serve --config wtc.yaml &
sleep 90                        # one poll interval + margin
wtc log --since 24h             # pipelines (build), merged MRs (merge), pushes
wtc doctor                      # per-source health + per-project watermarks
```

First run backfills 24h; each poll re-reads a 1h overlap so a pipeline still
running when last seen gets its terminal status — duplicates are impossible by
design (`dedup_key` upsert). `wtc doctor` lists `gitlab:pipelines`,
`gitlab:mrs`, `gitlab:commits` watermarks per project.

## 3. Webhook (optional — needs a public endpoint)

GitLab cannot HMAC-sign webhook bodies; it sends a static secret verbatim in
the `X-Gitlab-Token` header. wtc compares it in constant time (same shape as
Argo CD's `X-WTC-Token`).

```yaml
sources:
  gitlab:
    webhook_secret: ${WTC_GITLAB_WEBHOOK_SECRET}   # unset ⇒ endpoint fails closed (503)
```

Project (or group) → **Settings → Webhooks → Add new webhook**:

- URL: `https://wtc.example.com/ingest/gitlab`
- Secret token: the value of `WTC_GITLAB_WEBHOOK_SECRET`
- Triggers: **Pipeline events**, **Push events**, **Merge request events**
- Enable SSL verification (recommended when wtc has a real certificate).

A `401` means the token doesn't match; a `503` means `webhook_secret` is unset.
A push hook fans out to one event per commit; a non-merge MR action (open,
update, approve) is acknowledged and intentionally dropped.

### Webhook + poller together

Recommended when a public endpoint exists: the webhook gives latency, the
poller heals gaps (serve downtime, delivery loss). Dedup keys derive from
pipeline id / MR iid / commit sha, so both paths land on the same rows.

## 4. Env inference

GitLab events carry `source: gitlab` with repo/branch/event/paths/actor facts —
the same path-glob env rules that route GitHub route GitLab. For the operator's
kustomize-overlay layout:

```yaml
rules:
  - match: { source: gitlab, paths: ["**/overlays/dev/**"] }
    set:   { env: dev }
  - match: { source: gitlab, paths: ["**/overlays/staging/**"] }
    set:   { env: staging }
  - match: { source: gitlab, paths: ["**/overlays/prod/**"] }
    set:   { env: prod }
```

Paths on a merge event come from **MR-diff enrichment** (the MR *changes* API);
a pipeline event has no file list, so pipelines land with `env=""` unless a
non-path rule (e.g. branch) applies — expected, and surfaced by `wtc doctor`,
never guessed. A push touching only root files resolves to `env=""` honestly
(the file set is known, it just matches no overlay).

## 5. The tag↔sha join (`wtc where`)

Image tags that embed the git sha (`sha-<shortsha>`, `<semver>-<sha>`) match
wtc's default `tag_patterns`, so `wtc where <sha>` spans the GitLab flow with no
extra config:

```
$ wtc where sha-190b65d7
190b65d788aebbb7b76029da0c40ef4b69871620
  BUILD    succeeded  Pipeline #2682890363 success (main)          # GitLab CI built the image
  ENV dev
    intent   merge  MR !2 merged: promote image to sha-190b65d7    # GitLab MR bumped the overlay
    applied  succeeded  Application demo-gitlab-dev: sync Succeeded  (lag 31s)   # GitOps applied it
```

The INTENT→APPLIED link is the merge commit sha: the promotion MR's merge
commit is the revision Flux/Argo reconciles, and MR-diff enrichment records the
`newTag` bump so the image sha resolves back to the merge.

## 6. GitLab CI image tags

For the join to work, tag built images with the commit sha. Example
`.gitlab-ci.yml` build stage (kaniko, no privileged docker-in-docker):

```yaml
build-image:
  stage: build
  image: { name: "gcr.io/kaniko-project/executor:debug", entrypoint: [""] }
  script:
    - /kaniko/executor
      --context "${CI_PROJECT_DIR}"
      --dockerfile "${CI_PROJECT_DIR}/Dockerfile"
      --destination "${CI_REGISTRY_IMAGE}:sha-${CI_COMMIT_SHORT_SHA}"
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
```

## Notes / troubleshooting

- **`gitlab poller disabled`** in the log means one of `api_token`,
  `poll_interval > 0`, or a non-empty `projects` list is missing.
- **gitlab.com shared runners** require account identity verification before
  they execute pipelines. Without it, register a self-hosted runner
  (`gitlab-runner` container, docker executor) against the project and disable
  shared runners — pipelines then run locally.
- **Self-managed GitLab** blocks webhooks to local/loopback URLs by default
  (SSRF protection); wtc must be reachable at a routable address, or use the
  poller only.
