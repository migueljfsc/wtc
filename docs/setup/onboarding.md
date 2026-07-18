# Onboarding: run wtc in your cluster and wire GitHub + your GitOps engine

End-to-end setup for a fresh cluster: install the Helm chart, connect the
**GitHub API poller** (primary ingest — outbound only), and connect whichever
GitOps engine(s) deploy your workloads — **Flux** (Step 4), **Argo CD**
(Step 4b), or both. wtc is vendor-neutral: the engines are peers, and every
step below marks what belongs to which. By the end, `wtc log` and the portal
show real builds, merges, and deploys.

This guide wires GitHub, but the SCM/CI layer is vendor-neutral too: **GitLab**
is a first-class peer (poller + webhook, same Events and queries) — see
[gitlab.md](gitlab.md) to wire it instead of, or alongside, GitHub.

For deeper reference on each source see
[github-poller.md](github-poller.md), [gitlab.md](gitlab.md),
[flux.md](flux.md), and [argocd.md](argocd.md); this guide is the happy path
that ties them together with the chart.

## How the pieces fit

```
   GitHub API  ──(outbound HTTPS, poller)──▶  ┌──────────┐
                                              │   wtc    │ ── portal (/) + API (/api)
   Flux (per cluster) ──(notification POST)─▶ │  (Helm)  │
   Argo CD (per instance) ──(notifications)─▶ └──────────┘
```

wtc runs **once**. GitHub is pulled by the poller (no inbound needed). Your
GitOps engine posts to wtc from wherever it runs: same cluster → wtc's
in-cluster Service; other clusters → wtc's ingress URL. All ingest is outbound
from the source's side — no webhooks required.

## Ingest posture

For the SCM/CI sources (GitHub, GitLab), pick per installation based on whether
wtc has a public endpoint. Both sources support **both** modes, converging on
the same rows, so the choice is about reachability and latency, not coverage:

- **Private wtc (no public endpoint) → poller-primary.** Outbound HTTPS only;
  the poller pulls builds/merges/pushes every interval. This is the default and
  the operator's own posture. Webhooks are simply not wired.
- **Public wtc → webhooks + poller sweeper.** Register the webhook for
  low-latency ingest (a merge appears in seconds, not a poll interval) *and*
  keep the poller running as the loss-recovery sweeper. Dedup keys derive from
  run/PR/commit identity, so a change seen by both lands on one row — running
  both together produces zero duplicates.

GitOps notifications (Flux, Argo CD) are unaffected either way: that traffic is
in-cluster/outbound from the engine regardless of wtc's exposure.

## Prerequisites

- A cluster + `kubectl` and `helm`, and permission to create Secrets/Ingress.
- Your GitOps engine running where your workloads deploy: **Flux v2.x**
  (notification-controller) and/or **Argo CD** (notifications-controller,
  bundled in modern versions).
- A **GitHub fine-grained PAT**, read-only, scoped to the repos to watch:
  Actions + Contents + Pull requests + Metadata. (Classic PAT alternative:
  `repo`.)
- An **API token** you invent — the portal/CLI credential (any strong string).
- *(Flux)* an **HMAC key** you invent — a shared *webhook-signing* secret (any
  strong random string). It is **not** issued by Flux or anyone; you generate
  it and install the **same value** on both sides. Flux signs every delivery to
  `/ingest/flux` with it (`X-Signature: sha256=…`) and wtc verifies the
  signature, so only *your* Flux can post events. Same idea as a GitHub webhook
  secret. (The GitHub PAT above, by contrast, *is* issued by GitHub — it's a
  separate thing; the poller pulls GitHub, so nothing is signed there.)
- *(Argo CD)* a **webhook token** you invent — same idea as the HMAC key, but a
  plain shared secret: Argo's notification templates can't HMAC-sign bodies, so
  deliveries to `/ingest/argocd` carry it verbatim in an `X-WTC-Token` header
  and wtc compares it constant-time.

Generate the secrets you own (skip the engine you don't run):

```bash
API_TOKEN=$(openssl rand -hex 24)
FLUX_HMAC=$(openssl rand -hex 24)      # Flux
ARGOCD_TOKEN=$(openssl rand -hex 24)   # Argo CD
```

## Step 1 — Create the secret

The chart reads tokens from an existing Secret whose keys become env vars; the
config references them as `${VAR}` (an **unset** referenced var is a fatal
startup error by design).

```bash
kubectl create namespace wtc

kubectl -n wtc create secret generic wtc-secrets \
  --from-literal=WTC_API_TOKEN="$API_TOKEN" \
  --from-literal=WTC_GH_API_TOKEN="ghp_your_github_pat" \
  --from-literal=WTC_FLUX_HMAC_KEY="$FLUX_HMAC" \
  --from-literal=WTC_ARGOCD_WEBHOOK_SECRET="$ARGOCD_TOKEN"
```

Include only the keys for the engine(s) you run — each key pairs with its
`sources.*` block in Step 2 (a referenced `${VAR}` missing from the Secret is a
fatal startup error, so drop or keep block and key **together**).

## Step 2 — Write `values.yaml`

```yaml
existingSecret: wtc-secrets

# Deploy the portal (default) and expose both on one host, same-origin.
ingress:
  enabled: true
  className: nginx
  host: wtc.example.com            # your ingress host

# Rendered verbatim to /etc/wtc/wtc.yaml. ${VAR} come from wtc-secrets above.
config:
  server:
    listen: ":8484"
    db: /data/wtc.db

  auth:
    api_tokens:
      - ${WTC_API_TOKEN}           # portal/CLI bearer token

  sources:
    github:
      api_token: ${WTC_GH_API_TOKEN}
      poll_interval: 60s
      repos:                       # owner/name of each repo to watch —
        - your-org/app-api         #   OMIT this whole list to auto-discover
        - your-org/app-web         #   every repo the token can access
      infra_path: infrastructure/  # per-repo manifests dir (kustomize layout)
    # GitOps engines are peers — keep the block(s) for what you run, delete
    # the rest (together with its wtc-secrets key; see Step 1).
    flux:                            # if you run Flux (Step 4)
      hmac_key: ${WTC_FLUX_HMAC_KEY}
      suppression_window: 10m
    argocd:                          # if you run Argo CD (Step 4b)
      webhook_secret: ${WTC_ARGOCD_WEBHOOK_SECRET}
      suppression_window: 10m

  # Env/service inference (SPEC §3). Without rules, events land with env="".
  # Keep the rule groups for the engine(s) you run.
  rules:
    # GitHub: per-service CI workflow -> service (repo name minus the org).
    - match: { source: github, event: workflow_run }
      set:   { kind: build, service: "{{ trimPrefix .Repo \"your-org/\" }}" }
    # Flux: cluster name -> env (cluster-per-env). One per cluster you track.
    - match: { source: flux, cluster: dev }
      set:   { env: dev }
    - match: { source: flux, cluster: staging }
      set:   { env: staging }
    - match: { source: flux, cluster: prod }
      set:   { env: prod }
    # Flux: the reconciled object's name -> service.
    - match: { source: flux }
      set:   { service: "{{ .ObjectName }}" }
    # Argo CD: NEVER cluster=env (one Argo manages many clusters). Ordered
    # tiers: env app label > destination namespace > app-name suffix — this is
    # the minimal shape; see argocd.md for the full block.
    - match: { source: argocd }
      set:   { env: "{{ .EnvLabel }}" }   # empty render falls through
    - match: { source: argocd, namespace: prod }
      set:   { env: prod }
    - match: { source: argocd, object_name: "*-prod" }
      set:   { env: prod, service: '{{ trimSuffix .ObjectName "-prod" }}' }
    - match: { source: argocd }
      set:   { service: "{{ .ObjectName }}" }

  # tag_patterns default to sha-<shortsha> and <semver>-<sha>; only set this if
  # your image tags use a different convention (SPEC §2).
```

Notes:
- `auth.api_tokens` must be set or the API rejects everything.
- **`repos` is optional** — omit it to watch every repo the token can access
  (owner + collaborator + org member; archived repos skipped). The set is
  re-checked each poll, so repos added to the token appear automatically. Mind
  the rate budget: each repo costs ~3 requests per poll.
- GitHub build/push events legitimately land `env=""` — REST payloads carry no
  changed-file list and wtc never guesses. **Env comes from your GitOps
  engine** (Flux: cluster→env rules; Argo CD: label/namespace/name tiers) and
  the tag↔sha join (`wtc where`).

### Secrets: `existingSecret` vs `env`/`secretKeyRef`

Step 1 used `existingSecret`, which requires the Secret's keys to be named
`WTC_*`. If your secrets already exist under different names, use `env` instead
to map any Secret + key to the env var — no renaming, no `${VAR}` in the config
needed for source tokens (the `WTC_*` env override sets them directly):

```yaml
# values.yaml (instead of, or alongside, existingSecret)
env:
  - name: WTC_GH_API_TOKEN
    valueFrom:
      secretKeyRef: { name: github-credentials, key: token }
  - name: WTC_API_TOKEN
    valueFrom:
      secretKeyRef: { name: wtc-api-token, key: token }
  - name: WTC_FLUX_HMAC_KEY
    valueFrom:
      secretKeyRef: { name: wtc-flux-hmac, key: token }
```

## Step 3 — Install

```bash
helm upgrade --install wtc deploy/helm/wtc -n wtc -f values.yaml
kubectl -n wtc rollout status deploy/wtc
```

This creates the `wtc` API (Service `wtc:8484`), the `wtc-ui` portal, and the
Ingress. Open `https://wtc.example.com` and sign in with your `API_TOKEN`. The
poller starts immediately; within one interval (~60s + a 24h first-run
backfill) GitHub builds/merges appear in the timeline.

## Step 4 — Wire Flux (per cluster, if you run Flux)

For **each** cluster whose Flux you want to track, apply a Provider + Alert +
Secret. Start from [`flux-provider.yaml`](flux-provider.yaml) and edit three
things:

1. **Secret `token`** = the **same** value as `WTC_FLUX_HMAC_KEY` (deliveries
   are HMAC-SHA256 signed).
2. **`Provider.spec.address`** = wtc's `/ingest/flux` URL reachable **from that
   cluster**:
   - same cluster as wtc → the in-cluster Service:
     `http://wtc.wtc.svc.cluster.local:8484/ingest/flux`
     (`http://<release>.<namespace>.svc.cluster.local:8484/ingest/flux`)
   - a different cluster → wtc's ingress:
     `https://wtc.example.com/ingest/flux`
3. **`Alert.spec.eventMetadata.cluster`** = that cluster's name — it is how wtc
   knows which cluster (and via your rules, which **env**) an event belongs to.
   Use the same names as your rules (`dev`/`staging`/`prod`).

```bash
kubectl apply -f flux-provider.yaml   # edited, into flux-system on each cluster
```

Because wtc runs once, every cluster's Flux points at the **same** wtc address
but tags its events with its **own** `cluster` name — that is what
cluster-per-env inference relies on.

## Step 4b — Wire Argo CD (per instance, if you run Argo CD)

Argo CD has **no fixed webhook schema** — its notifications-controller sends
whatever the operator templates. wtc therefore ships the contract:
[`argocd-notifications.yaml`](argocd-notifications.yaml) contains the webhook
service, the canonical body template the parser targets, and the triggers.
Apply it per Argo instance and edit two things:

1. **`argocd-notifications-secret`'s `wtc-token`** = the same value as
   `WTC_ARGOCD_WEBHOOK_SECRET`.
2. **The webhook `url`** = wtc's `/ingest/argocd` address reachable from the
   Argo cluster — in-cluster Service or ingress, same rules as Flux (Step 4.2).

Unlike Flux there is **no cluster→env mapping**: one Argo instance manages many
clusters and its "cluster" is a destination server URL. Env comes from the
ordered tiers in your rules (label > namespace > name suffix — see
[argocd.md](argocd.md), including its troubleshooting section for the
notification gotchas found live).

## Step 5 — Verify

```bash
# Point the CLI at wtc (or just use the portal).
export WTC_SERVER=https://wtc.example.com WTC_API_TOKEN="$API_TOKEN"

wtc doctor                      # per-source health: github + your engine(s), watermarks
wtc log --since 24h             # builds/merges from GitHub

# Force a deploy event from your engine:
flux reconcile kustomization <name> -n flux-system                  # Flux
argocd app sync <app>           # Argo CD (or sync from its UI)
wtc log --kind deploy --since 10m                     # the sync/reconcile shows up
wtc diff staging prod           # once a service is deployed to both
```

In the portal: **Settings → Source health** shows the same counts; the
**Dashboard** and **Timeline** update live over SSE.

Signs it's working:
- `wtc doctor` shows non-zero `github` counts and advancing poll watermarks.
- After a reconcile/sync, a `flux`/`argocd` `deploy` event appears with the
  right `env`.
- If events land with `env=""`: for Flux, `eventMetadata.cluster` doesn't match
  a rule — fix the cluster name or the rule; for Argo CD, no inference tier
  matched — label the app, map the namespace, or add a name-suffix rule.

## Tuning env/service inference

Inference is the product's hard problem — everything flows through the ordered
rules engine (SPEC §3), and unmatched events surface in `wtc doctor` rather than
being guessed. You can **edit rules live** in the portal (Settings → Edit):
saved rules are validated and hot-reloaded, and the **next** ingested event is
routed by them — no restart, and it works even though the chart mounts the
config read-only (the edit is stored in the DB).

## Multi-cluster (cluster-per-env)

The operator model is one cluster per env (`dev`/`staging`/`prod`), wtc deployed
once. With Flux, wire each cluster's notification-controller to the single wtc
address (Step 4), each tagging its own `cluster` name. With Argo CD the shape
inverts — one instance already manages many clusters, so a single Step 4b
wiring covers all of them and env comes from the app tiers, not the cluster.
The GitHub poller is global (it watches repos, not clusters). `wtc diff staging
prod` and the portal's env matrix then compare what's actually running per env.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Pod crashloops on start: "unset environment variable" | The config references a `${VAR}` not in `wtc-secrets`. Add the key. |
| API returns 401 everywhere | `auth.api_tokens` empty, or wrong token. |
| `/ingest/flux` → 503 | `sources.flux.hmac_key` unset (fails closed). |
| `/ingest/argocd` → 503 | `sources.argocd.webhook_secret` unset (fails closed). |
| Flux events never arrive | Provider `address` unreachable from that cluster, or the Secret `token` ≠ `WTC_FLUX_HMAC_KEY` (HMAC mismatch → rejected). |
| Argo events never arrive | Wrong recipient format, a `notified.…` annotation suppressing re-sends, or template errors — see [argocd.md](argocd.md) §Troubleshooting. |
| Flux events arrive with `env=""` | `eventMetadata.cluster` doesn't match any `cluster→env` rule. |
| Argo events arrive with `env=""` | No inference tier matched (no `env` label, unmapped namespace, no name suffix) — add a rule; never inferred from cluster. |
| GitHub builds have `env=""` | Expected — env comes from Flux + the tag↔sha join, not build events. |
| Portal loads but can't reach the API | Using the cross-origin model without CORS; prefer the same-origin ingress (default) or set `ui.apiBaseUrl` + `config.server.cors.allowed_origins`. See [portal.md](portal.md). |

## Related

- [github-poller.md](github-poller.md) · [github-webhook.md](github-webhook.md)
  (public-endpoint webhook mode)
- [flux.md](flux.md) · [flux-provider.yaml](flux-provider.yaml)
- [argocd.md](argocd.md) · [argocd-notifications.yaml](argocd-notifications.yaml)
- [multi-cluster.md](multi-cluster.md) (one central hub for N clusters)
- [portal.md](portal.md) (portal deploy + auth) · [deploy.md](deploy.md)
- [retention.md](retention.md) · [alertmanager.md](alertmanager.md) (alert
  correlation) · [slack-digest.md](slack-digest.md)
