# wtc — Technical Specification (v1)

Lives at `docs/SPEC.md`. Authoritative for schema, config, API, and query semantics. Change via PR only.

## 1. Event model

One table. One row = one logical change. Status transitions update the row in place (keyed by `dedup_key`).

```sql
CREATE TABLE events (
  id          TEXT PRIMARY KEY,            -- ULID
  ts          TEXT NOT NULL,               -- RFC3339 UTC, source event time
  ingested_at TEXT NOT NULL,               -- RFC3339 UTC
  source      TEXT NOT NULL,               -- github | flux | helm | terraform | manual | generic | alertmanager
  kind        TEXT NOT NULL,               -- build | merge | push | deploy | config_change | infra_change | rollback | alert | manual
  status      TEXT NOT NULL DEFAULT 'unknown', -- started | succeeded | failed | unknown
  env         TEXT NOT NULL DEFAULT '',    -- prod | staging | dev | pr-123 | '' (unmapped)
  cluster     TEXT NOT NULL DEFAULT '',
  namespace   TEXT NOT NULL DEFAULT '',
  service     TEXT NOT NULL DEFAULT '',
  actor       TEXT NOT NULL DEFAULT '',    -- human login, bot name, or 'flux'
  ref         TEXT NOT NULL DEFAULT '',    -- git sha / revision (manifest repo revision for flux events)
  artifact    TEXT NOT NULL DEFAULT '',    -- primary artifact, e.g. registry/app:tag or chart@version
  title       TEXT NOT NULL,               -- one line, human-readable
  url         TEXT NOT NULL DEFAULT '',    -- deep link into the source system
  duration_ms INTEGER,
  dedup_key   TEXT NOT NULL UNIQUE,
  payload     TEXT                         -- redacted raw JSON; may include "artifacts": [...]
);
CREATE INDEX idx_events_ts         ON events(ts);
CREATE INDEX idx_events_env_ts     ON events(env, ts);
CREATE INDEX idx_events_service_ts ON events(service, ts);
CREATE INDEX idx_events_ref        ON events(ref);
CREATE INDEX idx_events_kind_ts    ON events(kind, ts);
```

Full-text search: FTS5 external-content table over `(title, service, actor, artifact)` maintained by triggers; backs `wtc log -q <text>`.

Upsert rule: `INSERT ... ON CONFLICT(dedup_key) DO UPDATE` — only when the incoming status **strictly outranks** the stored one (`unknown < started < succeeded|failed`; equal rank never overwrites, so a stale terminal replay cannot flip `succeeded↔failed` or move `ts` backward). On update: `status`, `ts`, `title` always; `duration_ms`, `payload`, `url`, and identity fields (`env`, `cluster`, `namespace`, `service`, `actor`, `ref`, `artifact`) follow **non-empty-wins merge** — a later event enriches the row but never blanks what an earlier event recorded. `kind` and `source` are set by the first event and never updated.

### kind semantics

| kind | meaning | typical producer |
|---|---|---|
| build | CI produced/attempted an artifact | GH `workflow_run` on app repos |
| merge | change intent approved | GH `pull_request` closed+merged |
| push | direct commit (incl. flux image-automation bot) | GH `push` on manifests repo |
| deploy | change applied to a runtime env | Flux reconcile success; `wtc wrap -- helm ...` |
| config_change | non-image config edit reaching an env | manifests-repo changes not matching a tag bump |
| infra_change | cloud/infra mutation | `wtc wrap -- terraform apply`; Crossplane-related manifests |
| rollback | explicit revert | detected revert PRs (phase ≥4) or manual |
| alert | monitoring signal (correlation only) | Alertmanager webhook (phase 5) |
| manual | anything a human records | `wtc record` |

### dedup_key derivation (stable, source-side)

- github workflow_run → `gh:run:<repo>:<run_id>:<run_attempt>`
- github pull_request merged → `gh:pr:<repo>:<number>:merged`
- github push → `gh:push:<repo>:<after_sha>`
- flux → `flux:<cluster>:<kind>/<ns>/<name>:<revision>:<reason>`
- wrap/record → `local:<ulid>` generated at start, reused for the completion update (a `wtc record` retry without an explicit `--dedup-key` is a NEW event)
- alertmanager → `am:<fingerprint>:<startsAt>`

## 2. Configuration (`wtc.yaml`)

```yaml
server:
  listen: ":8484"
  db: /var/lib/wtc/wtc.db
  base_url: https://wtc.example.com     # used in links/digests
  capture_dir: ""                        # non-empty => dump raw ingest bodies (dev only)

auth:
  api_tokens:                            # bearer tokens for /api/* and /ingest/generic
    - ${WTC_API_TOKEN}

sources:
  github:
    webhook_secret: ${WTC_GH_WEBHOOK_SECRET}   # only needed if webhooks are wired (public endpoint)
    api_token: ${WTC_GH_API_TOKEN}       # enables the poller (primary ingest when private) + PR-diff enrichment
    poll_interval: 60s                   # 0 disables the poller (webhook-only mode)
    repos:                               # poller scope; webhooks accept any repo passing HMAC
      - org/app-api
      - org/app-web
    infra_path: infrastructure/          # per-repo manifests dir (microservices layout)

  flux:
    hmac_key: ${WTC_FLUX_HMAC_KEY}
    suppression_window: 10m

tag_patterns:                            # ordered; first regex with a <sha> group that matches an image tag wins
  - '^sha-(?P<sha>[0-9a-f]{7,40})$'                     # sha-abc1234
  - '^v?\d+\.\d+\.\d+-(?P<sha>[0-9a-f]{7,40})$'         # 1.4.2-abc1234

rules:                                   # ordered; see §3
  - match: { source: flux, cluster: prod }
    set:   { env: prod }
  - match: { source: flux, cluster: staging }
    set:   { env: staging }
  - match: { source: flux, cluster: dev }
    set:   { env: dev }
  - match: { source: github, event: workflow_run }
    set:   { kind: build, service: "{{ trimPrefix .Repo \"org/\" }}" }
  - match: { source: github, paths: ["infrastructure/overlays/prod/**"] }
    set:   { env: prod, service: "{{ trimPrefix .Repo \"org/\" }}" }
  - match: { source: github, paths: ["infrastructure/overlays/staging/**"] }
    set:   { env: staging, service: "{{ trimPrefix .Repo \"org/\" }}" }
  - match: { source: github, paths: ["infrastructure/overlays/dev/**"] }
    set:   { env: dev, service: "{{ trimPrefix .Repo \"org/\" }}" }
  - match: { source: flux, object_kind: HelmRelease }
    set:   { service: "{{ .ObjectName }}" }

retention:
  keep: 180d
  ephemeral_env_pattern: "pr-*"
  ephemeral_keep: 30d
```

Env expansion `${VAR}` at load. `WTC_SERVER_LISTEN`-style overrides win over file values.

## 3. Normalization rules engine

Runs after each source-specific parser produces a partially-filled Event plus a **fact map** (repo, branch, event, paths[], cluster, object_kind, object_name, namespace, actor, reason…).

- Rules evaluate **in order**. A rule matches when every `match` key matches its fact (globs `*`/`**` allowed on strings and path lists; `paths` matches if ANY changed path matches ANY pattern).
- A matching rule sets only fields **not yet set** (first-writer-wins per field). Evaluation continues through all rules (no short-circuit) so later rules can fill remaining fields.
- `set` values support minimal Go templates over the fact map: `trimPrefix`, `trimSuffix`, `lower`, `regexReplace`.
- Truncated path lists (GitHub cap) ⇒ path-based matches are skipped for that event, not treated as non-matching; event lands with `env=""` and is counted by `doctor`.
- After rules: redaction pass over `title` and `payload`.

## 4. HTTP surface

Ingest (serve only):

```
POST /ingest/github     HMAC X-Hub-Signature-256 (webhook_secret)
POST /ingest/flux       HMAC X-Signature (flux generic-hmac)
POST /ingest/generic    Bearer token; body = Event JSON subset (kind, title, env?, service?, cluster?, namespace?, actor?, ts?, ref?, artifact?, artifacts?, status?, duration_ms?, url?, source?, dedup_key?)
                        source restricted to generic|manual|helm|terraform; dedup_key prefixes gh:/flux:/am: rejected (reserved for dedicated ingest paths).
                        Omitting dedup_key ⇒ server generates a random key: the delivery is NOT idempotent — clients needing retry-safety must send a stable key.
POST /ingest/alertmanager   Bearer token (phase 5)
GET  /healthz
```

Query API (Bearer):

```
GET /api/events?env=&service=&kind=&status=&since=&until=&q=&limit=&cursor=
GET /api/where/{ref}          # ref = full/short sha or image tag
GET /api/diff?a=staging&b=prod
GET /api/handoff?since=168h
GET /api/doctor
```

All timestamps RFC3339. Cursor pagination on `(ts, id)`.

## 5. CLI surface

```
wtc init                        # scaffold wtc.yaml, print wiring checklists
wtc serve [--config wtc.yaml]
wtc log [--env E] [--service S] [--kind K] [--since 2h] [--until T] [-q text] [--json]
wtc where <sha|tag> [--json]
wtc diff <envA> <envB> [--json]
wtc handoff [--since 7d] [--json]
wtc record --kind K --env E --service S --title "..." [--ref R] [--artifact A] [--status S] [--ts T]
wtc wrap [--env E] [--service S] -- <command...>
wtc doctor [--json]
```

Client resolution: `--server`/`WTC_SERVER` + `WTC_API_TOKEN`; default `http://localhost:8484`.

### `wtc wrap` behavior

1. Emits `started` event (dedup_key `local:<ulid>`), runs the command inheriting stdio, then upserts `succeeded|failed` with `duration_ms` and exit code in payload.
2. Arg sniffers prefill fields when flags are absent:
   - `helm upgrade|install <release> <chart>` → kind=deploy, service=release, namespace from `-n`, artifact=chart, plus `--set image.tag=...` if present.
   - `terraform apply|destroy` → kind=infra_change; if stdout is the `-json` stream, count add/change/destroy into the title; **never** store plan/resource bodies.
3. If the server is unreachable: print a warning, still run the command, exit with the command's code. wtc must never block operations.

## 6. Query semantics

### `wtc log`
Filtered scan ordered by `ts desc`. Default `--since 24h`, limit 100. `-q` uses FTS5.

### `wtc where <ref>`
Composed picture of a change's journey:

1. **BUILD** — events kind=build with `ref = sha` or sha-prefix match, or `artifact/artifacts[]` containing `<ref>`. Image tags resolve to shas through the configured `tag_patterns` list (§2), so `where` accepts either form.
2. **INTENT** — merge/push events whose payload references the sha or the image tag produced by step 1 (tag set comes from build-event `artifacts[]`; enrichment in §7 makes this reliable).
3. **APPLIED** — deploy events per env whose `ref` equals the manifest-repo revision(s) from step 2, or whose `artifact` matches a step-1 tag.

Output: staged tree grouped BUILD → per-env (INTENT ts, APPLIED ts/status), with explicit `unknown` markers and intent→applied lag. Accepts an image tag as input (skips step 1).

### `wtc diff <a> <b>`
Per service present in either env: latest `deploy` with status=succeeded in each; compare `artifact` (fallback `ref`). Columns: service, a-artifact, b-artifact, drift age (ts delta), last actor. Flag services deployed in exactly one env. Explicit caveat in output when an env's latest deploy lacks artifact data ("revision-only comparison").

### `wtc handoff --since 7d`
Digest: deploys per env (count, failures list), infra_changes, rollbacks, unmapped-event count, top actors, first-seen services, alerts (once phase 5). Markdown to stdout (pipeable to Slack later).

### `wtc doctor`
Per source: last event age, 24h counts, dedup-drop counts, unmapped (`env=""`) counts with 3 sample titles, clock-skew flags, db size, retention stats. Exit non-zero if any source silent > threshold.

## 7. GitHub API integration (requires `api_token`)

**Poller — primary ingest when wtc has no public endpoint.** Every `poll_interval`, for each configured repo: list workflow runs, merged PRs, and default-branch commits since the per-repo high-water mark (persisted in the DB); normalize through the same parsers and rules pipeline as webhooks. Idempotent via dedup_key, so poller and webhooks can run simultaneously — the poller doubles as the webhook-loss sweeper. First run backfills a bounded window (default 24h).

**PR-diff enrichment.** On merged PRs touching `infra_path`: fetch changed files, extract image-tag bumps via configurable regexes (defaults: `tag:\s*["']?(?P<new>\S+)` on YAML, `newTag:\s*(?P<new>\S+)` for kustomize), store old→new tags in payload. This creates the tag↔manifest-revision link that `where` step 2/3 depends on. Diff bodies beyond matched lines are not stored.

## 8. Retention

Daily job: delete events older than `keep` (`ephemeral_keep` for envs matching `ephemeral_env_pattern`), then `PRAGMA incremental_vacuum`.

## 9. Wiring artifacts to ship in docs/setup/

- `github-webhook.md` — org/repo webhook: URL `/ingest/github`, content-type json, secret, events: `workflow_run, push, pull_request` (+ `release` optional).
- `flux-provider.yaml` — per cluster: `Provider` (type generic-hmac → `/ingest/flux`, secretRef) + `Alert` (eventSeverity: info, sources: Kustomization/*, HelmRelease/*, ImageUpdateAutomation/*) + note to set a cluster identifier (Alert `eventMetadata` or summary) so the fact map carries `cluster`. Validate exact field names against captured fixtures before finalizing.
- `gha-report-step.md` — optional composite-action/curl step POSTing `/ingest/generic` with `{kind: build, ref: $GITHUB_SHA, artifacts: [...]}` for pipelines whose tags don't embed the sha (not needed by the operator; kept for generality).
- `Dockerfile` + Helm chart under `deploy/helm/` (primary, in-cluster) + `deploy/docker-compose.yaml` (VMs/local).
