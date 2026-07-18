# Wiring Flux notification-controller

Flux pushes reconcile events to wtc from inside the cluster — outbound only,
works in fully private deployments.

## 1. Config (wtc side)

```yaml
sources:
  flux:
    hmac_key: ${WTC_FLUX_HMAC_KEY}
    suppression_window: 10m     # drop re-emits of the same (object,revision,reason)
```

Unset key ⇒ `/ingest/flux` fails closed (503).

## 2. Cluster side

Apply [`flux-provider.yaml`](flux-provider.yaml) per cluster after editing:

- the Secret `token` (= `hmac_key`; deliveries are HMAC-SHA256 signed via
  `X-Signature: sha256=<hex>`)
- `Provider.spec.address` — wtc's `/ingest/flux` URL reachable from the cluster
- `Alert.spec.eventMetadata.cluster` — the cluster's name; a rule like
  `match: {source: flux, cluster: prod} → set: {env: prod}` turns it into env

## 3. Rules that make Flux events useful

```yaml
rules:
  - match: { source: flux, cluster: prod }
    set:   { env: prod }
  - match: { source: flux }
    set:   { service: "{{ .ObjectName }}" }
```

## 4. Verify

```bash
flux reconcile kustomization <name> -n flux-system
wtc log --env <env> --kind deploy --since 10m
```

## Scope: keep third-party reconciles out of the ledger

A cluster's notification-controller fires for *everything* Flux reconciles —
cert-manager, external-dns, operator CRDs, and so on. Restrict what enters the
ledger with an allow/deny list matched on **raw facts** (`namespace`,
`object_name` = involvedObject.name, `object_kind`, `cluster`); it is the
push-source analog of the GitHub/GitLab poller's repo scope.

```yaml
sources:
  flux:
    hmac_key: ${WTC_FLUX_HMAC_KEY}
    suppression_window: 10m
    scope:
      allow:                         # empty ⇒ ingest everything
        - { namespace: "team-*" }    # fields within an entry are AND
        - { object_kind: "HelmRelease", namespace: "apps" }
      deny:                          # empty ⇒ deny nothing; deny wins over allow
        - { namespace: "flux-system" }
        - { object_name: "*-canary" }
```

Semantics: **deny wins** over allow; an empty `allow` allows everything; an
empty `deny` denies nothing; fields within one entry are AND, entries are OR.
Globs use `*` (one segment) / `**` (any depth). Non-matching events are dropped
at ingest (never stored) and counted by `wtc_filtered_total{source="flux"}`;
a bad pattern or an all-empty entry fails `wtc serve` at startup.

> This is a scope declaration, not a query filter — dropped events are not
> stored, and Flux has no poller to backfill them, so widening the scope later
> does not recover past events.

## Behavior notes (from captured payloads)

- Kustomization events carry `metadata.revision` like
  `master@sha1:<full-sha>`; wtc extracts the bare sha into `ref` — the join
  `wtc where` uses. HelmRelease revisions are chart versions and land in
  `artifact` as `<release>@<version>`.
- severity `info` → status succeeded, `error` → failed. Reconciles never
  produce `started`.
- notification-controller re-emits on every reconcile interval. The
  suppression window sheds these before the write path; even with it
  disabled, the dedup upsert stores exactly one row per
  (object, revision, reason).
- Image-automation events (`ImageUpdateAutomation`) are covered by the Alert
  but their normalizer fixture is pending capture from a cluster with image
  automation running (needs a writable manifests repo).
