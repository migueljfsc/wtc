# Onboarding: run wtc in your cluster and wire GitHub + Flux

End-to-end setup for a fresh cluster: install the Helm chart, connect the
**GitHub API poller** (primary ingest — outbound only), and connect **Flux
notification-controller** (in-cluster reconcile events). By the end, `wtc log`
and the portal show real builds, merges, and deploys.

For deeper reference on each source see
[github-poller.md](github-poller.md) and [flux.md](flux.md); this guide is the
happy path that ties them together with the chart.

## How the pieces fit

```
   GitHub API  ──(outbound HTTPS, poller)──▶  ┌──────────┐
                                              │   wtc    │ ── portal (/) + API (/api)
   Flux (this cluster) ──(in-cluster POST)──▶ │  (Helm)  │
   Flux (other clusters) ──(via ingress)────▶ └──────────┘
```

wtc runs **once**. GitHub is pulled by the poller (no inbound needed). Flux in
the **same** cluster posts to wtc's in-cluster Service; Flux in **other**
clusters posts to wtc's ingress URL. All ingest is outbound from the source's
side — no webhooks required.

## Prerequisites

- A cluster + `kubectl` and `helm`, and permission to create Secrets/Ingress.
- **Flux v2.x** running in the clusters you want to track (notification-controller).
- A **GitHub fine-grained PAT**, read-only, scoped to the repos to watch:
  Actions + Contents + Pull requests + Metadata. (Classic PAT alternative:
  `repo`.)
- An **API token** you invent — the portal/CLI credential (any strong string).
- An **HMAC key** you invent — a shared *webhook-signing* secret (any strong
  random string). It is **not** issued by Flux or anyone; you generate it and
  install the **same value** on both sides. Flux signs every delivery to
  `/ingest/flux` with it (`X-Signature: sha256=…`) and wtc verifies the
  signature, so only *your* Flux can post events. Same idea as a GitHub webhook
  secret. (The GitHub PAT above, by contrast, *is* issued by GitHub — it's a
  separate thing; the poller pulls GitHub, so nothing is signed there.)

Generate the two secrets you own:

```bash
API_TOKEN=$(openssl rand -hex 24)
FLUX_HMAC=$(openssl rand -hex 24)
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
  --from-literal=WTC_FLUX_HMAC_KEY="$FLUX_HMAC"
```

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
    flux:
      hmac_key: ${WTC_FLUX_HMAC_KEY}
      suppression_window: 10m

  # Env/service inference (SPEC §3). Without rules, events land with env="".
  rules:
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
    # GitHub: per-service CI workflow -> service (repo name minus the org).
    - match: { source: github, event: workflow_run }
      set:   { kind: build, service: "{{ trimPrefix .Repo \"your-org/\" }}" }

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
  changed-file list and wtc never guesses. **Env comes from Flux** (the
  cluster→env rules) and the tag↔sha join (`wtc where`).

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

## Step 4 — Wire Flux (per cluster)

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

## Step 5 — Verify

```bash
# Point the CLI at wtc (or just use the portal).
export WTC_SERVER=https://wtc.example.com WTC_API_TOKEN="$API_TOKEN"

wtc doctor                      # per-source health: github + flux counts, watermarks
wtc log --since 24h             # builds/merges from GitHub
flux reconcile kustomization <name> -n flux-system    # force a reconcile
wtc log --env prod --kind deploy --since 10m           # the reconcile shows up
wtc diff staging prod           # once a service is deployed to both
```

In the portal: **Settings → Source health** shows the same counts; the
**Dashboard** and **Timeline** update live over SSE.

Signs it's working:
- `wtc doctor` shows non-zero `github` counts and advancing poll watermarks.
- After a reconcile, a `flux` `deploy` event appears with the right `env`.
- If Flux events land with `env=""`, your `eventMetadata.cluster` doesn't match
  a rule — fix the cluster name or the rule.

## Tuning env/service inference

Inference is the product's hard problem — everything flows through the ordered
rules engine (SPEC §3), and unmatched events surface in `wtc doctor` rather than
being guessed. You can **edit rules live** in the portal (Settings → Edit):
saved rules are validated and hot-reloaded, and the **next** ingested event is
routed by them — no restart, and it works even though the chart mounts the
config read-only (the edit is stored in the DB).

## Multi-cluster (cluster-per-env)

The operator model is one cluster per env (`dev`/`staging`/`prod`), wtc deployed
once. Wire each cluster's Flux to the single wtc address (Step 4), each tagging
its own `cluster` name. The GitHub poller is global (it watches repos, not
clusters). `wtc diff staging prod` and the portal's env matrix then compare
what's actually running per env.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Pod crashloops on start: "unset environment variable" | The config references a `${VAR}` not in `wtc-secrets`. Add the key. |
| API returns 401 everywhere | `auth.api_tokens` empty, or wrong token. |
| `/ingest/flux` → 503 | `sources.flux.hmac_key` unset (fails closed). |
| Flux events never arrive | Provider `address` unreachable from that cluster, or the Secret `token` ≠ `WTC_FLUX_HMAC_KEY` (HMAC mismatch → rejected). |
| Flux events arrive with `env=""` | `eventMetadata.cluster` doesn't match any `cluster→env` rule. |
| GitHub builds have `env=""` | Expected — env comes from Flux + the tag↔sha join, not build events. |
| Portal loads but can't reach the API | Using the cross-origin model without CORS; prefer the same-origin ingress (default) or set `ui.apiBaseUrl` + `config.server.cors.allowed_origins`. See [portal.md](portal.md). |

## Related

- [github-poller.md](github-poller.md) · [github-webhook.md](github-webhook.md)
  (public-endpoint webhook mode)
- [flux.md](flux.md) · [flux-provider.yaml](flux-provider.yaml)
- [portal.md](portal.md) (portal deploy + auth) · [deploy.md](deploy.md)
- [retention.md](retention.md) · [alertmanager.md](alertmanager.md) (alert
  correlation) · [slack-digest.md](slack-digest.md)
