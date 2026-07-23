# One central wtc for many clusters

**Yes — this is the design.** wtc is a single ledger fed over HTTP, so one
central instance ("the hub") records change events from any number of clusters.
Each cluster's Flux/Argo notification controller pushes to the hub, tagging its
events with a cluster name; the hub's rules turn that name into an environment.
`wtc where <sha>` then follows one commit across every cluster it reached, and
`wtc diff staging prod` compares two clusters that may live on different clouds.

```
   cluster: dev  ─┐   Flux notification-controller → POST /ingest/flux
   cluster: stg  ─┼─────────────────────────────────────────────►  ┌──────────────┐
   cluster: prod ─┘                                                 │  central wtc  │
                                                                    │  (one pod +   │
   Argo CD (manages many clusters) ── POST /ingest/argocd ────────► │   one DB)     │
                                                                    │              │
   GitHub / GitLab  ◄── the hub POLLS them (one poller, all repos) ─┤              │
                                                                    └──────────────┘
```

## What runs where

- **The hub**: one `wtc serve` (Helm chart, `deploy/helm/wtc`). Postgres backend
  recommended for a hub (stateless pod, standard backup) — see
  [postgres.md](postgres.md). Still **one replica**: pollers and suppression
  windows are per-pod.
- **Each spoke cluster**: only a Flux `Provider` + `Alert` (or Argo notifications
  config). No wtc component runs in the spokes — they just send events out.
- **SCM pollers run once, on the hub.** GitHub/GitLab are polled centrally, so
  one poller covers every repo regardless of how many clusters deploy it. The
  spokes never talk to the SCM on wtc's behalf.

## 1. Reachability

The spokes must reach the hub's ingest URL. The traffic is outbound-only from
each spoke (webhook POSTs), authenticated per source:

- Expose the hub via an Ingress/LoadBalancer with a hostname the spokes can
  resolve — public with TLS, or private (VPC peering, VPN, Tailscale, service
  mesh). `/ingest/*` is authenticated (Flux generic-hmac, Argo `X-WTC-Token`),
  so exposure is a deliberate per-install choice, not a security hole.
- If every cluster is in one mesh/VPC, an internal LoadBalancer or a
  mesh-routed hostname is enough — no public endpoint.
- The hub's **query/UI** surface (`/api/*`, portal) is separate and can stay
  private even when `/ingest/*` is reachable.

## 2. Wire each spoke's Flux

Apply [`flux-provider.yaml`](flux-provider.yaml) in every spoke, changing two
things per cluster:

- `Provider.spec.address` → the hub's `/ingest/flux` URL (same for all spokes).
- `Alert.spec.eventMetadata.cluster` → **this cluster's unique name**. This label
  is the only thing that tells the hub which cluster an event came from — make it
  distinct and stable (`prod-eu`, `prod-us`, `staging`, …).

Use the **same** `hmac_key` across spokes for simplicity, or a per-cluster key if
you want to be able to revoke one cluster's access independently — the hub's
`sources.flux.hmac_key` accepts one key today, so per-cluster keys mean fronting
the hub with distinct ingest paths (advanced; one shared key is the common
choice).

## 3. Map cluster → env on the hub

The hub's rules turn each cluster name into an environment. For a
cluster-per-env layout:

```yaml
rules:
  - match: { source: flux, cluster: prod-eu }
    set:   { env: prod }
  - match: { source: flux, cluster: prod-us }
    set:   { env: prod }
  - match: { source: flux, cluster: staging }
    set:   { env: staging }
  - match: { source: flux, cluster: dev }
    set:   { env: dev }
```

If your cluster names already equal your env names, one rule does it:
`match: { source: flux } → set: { env: "{{ .Cluster }}" }`. Rules hot-reload
from the portal's Configuration tab — no restart. Events from an unmapped
cluster land with `env=""` and surface in `wtc doctor`, never guessed.

**Many clusters per env is native.** An env is a logical grouping, not one
cluster — a `dev` env can span a `dev` cluster, a `tools` cluster, a `ci`
cluster, whatever you run. Map each to the same env and they aggregate as one
`dev` for the dashboard, `diff` and DORA. `cluster` is itself a first-class
**facet**: the scope bar, timeline, changes and `wtc log --cluster <name>`
all slice by it, so you can narrow any view to one cluster within an env
without splitting the env. The physical cluster is always preserved on the
event even when several share an env.

> Keep the physical cluster distinct from the logical env when they differ:
> two `prod-*` clusters both map to `env: prod`, and `wtc diff staging prod`
> then compares them as one prod. If you want to compare the two prod clusters
> against each other, map them to distinct envs (`prod-eu`/`prod-us`) instead.

## 4. Argo CD

Two topologies, both supported:

- **One Argo, many clusters** (the common case): a **single** Argo
  notifications config pointing at the hub's `/ingest/argocd` covers every
  managed cluster — see [argocd.md](argocd.md). Env inference uses the
  Application's `env` label, destination namespace, then name suffix — never
  the destination cluster URL, which is not an environment.
- **One Argo per env** (a team runs a `dev` Argo, a `prod` Argo, …): set the
  `cluster` field in each instance's notification template to a distinct,
  stable name (`argo-dev`, `argo-prod`, …). Unlike Flux's `eventMetadata`,
  Argo can't emit its own instance identity, so this is a static literal you
  fill in per install (the shipped template carries a `CHANGEME-…`
  placeholder). It lands in `Event.cluster` — a facet like any other — and in
  the rule facts, so an env fallback for apps that carry no `env` label is one
  rule: `- match: { source: argocd, cluster: argo-dev } → set: { env: dev }`.

## 5. Verify

```bash
# from the hub (or via the portal):
wtc doctor                     # sources: flux should show events from every cluster
wtc log --since 1h             # events tagged by cluster, mapped to env
wtc diff staging prod          # cross-cluster comparison
```

If a spoke is silent: check its `notification-controller` logs for POST
failures (usually reachability or a HMAC mismatch), and confirm its `Alert`
carries the `cluster` metadata (`kubectl -n flux-system get alert wtc -o yaml`).

## Scope note

This is horizontal fan-in to **one** logical wtc, not multi-tenancy: all
clusters share one ledger and one auth realm (no per-cluster RBAC — a non-goal
for v1). It stays a single pod and single DB; the clusters are event *sources*,
not shards.
