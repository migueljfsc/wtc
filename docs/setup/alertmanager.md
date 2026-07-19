# Wiring Alertmanager

Alerts become correlation anchors: `wtc around <alert>` shows what changed in
the window before an alert fired. Alerts are `kind=alert` — they never appear
in `diff` or `where`.

## wtc side

Nothing to configure — `/ingest/alertmanager` uses the same bearer tokens as
the rest of `/api/*` and `/ingest/generic` (`auth.api_tokens`).

## Alertmanager side

Add a webhook receiver pointing at wtc, authenticated with an API token:

```yaml
receivers:
  - name: wtc
    webhook_configs:
      - url: http://wtc.wtc-system.svc.cluster.local:8484/ingest/alertmanager
        http_config:
          authorization:
            credentials: <one of auth.api_tokens>
route:
  receiver: wtc          # or add wtc as a continue: true sibling route
```

The safest shape is a `continue: true` sibling route, so wtc observes alerts
without stealing them from your paging receivers:

```yaml
route:
  routes:
    - receiver: wtc
      continue: true
      # optionally add matchers: [...] here — see below
    # ...your existing paging routes...
```

## Which alerts are worth sending

wtc attaches no meaning to what an alert is about — every delivered episode
is one `kind=alert` row, and `blast` correlates purely on *when* and *where*
(timestamp, env, service). So the useful dividing line is: **could a change
wtc knows about — a deploy, config change, infra change — plausibly have
caused this alert within hours?**

High signal (the kube-prometheus-stack names; map to your own rules):

- `KubePodCrashLooping` — a new image/config that crashes on boot
- `KubeContainerWaiting` — `ImagePullBackOff` / image not found: a bad tag
  reached the manifests
- `KubeDeploymentReplicasMismatch`, `KubeStatefulSetReplicasMismatch`,
  rollout-stuck style alerts — a rollout that never converged
- `KubePodNotReady` — failing readiness after a change
- app-level SLO alerts (error rate, latency burn) **with a `service` label**
  — the classic "regression shipped" signal, and the service booster works

Low signal — capacity/hardware trends that a fresh change rarely explains:
node CPU/memory pressure, `NodeFilesystemSpaceFillingUp`, cert expiry,
backup-job failures, `Watchdog`/`InfoInhibitor` (never send these two — the
Watchdog fires forever by design and becomes a permanent timeline row).

A per-service `HighCpu`/`HighMemory` is the middle ground: as a *node* alert
it's noise, but with a `service` label it catches "the new version eats CPU"
— send it if it pages you anyway.

Filtering is Alertmanager's job (wtc has no alert-side scope config). Either
enumerate the high-signal names:

```yaml
    - receiver: wtc
      continue: true
      matchers:
        - alertname =~ "KubePodCrashLooping|KubeContainerWaiting|KubeDeploymentReplicasMismatch|KubeStatefulSetReplicasMismatch|KubePodNotReady|HighErrorRate|LatencyBurn.*"
```

or just send everything that pages (`severity =~ "warning|critical"`) and let
retention handle volume — over-sending is harmless (rows, not pages);
under-sending costs you the anchor exactly when you need it.

One duplication note: if the Flux (or Argo CD) notification source is wired,
reconcile failures already land as first-class `deploy/failed` events —
Prometheus alerts *about* Flux objects (`HelmReleaseNotReady`-style) add a
second, less precise row for the same fact. Skip them.

## Behavior (from a captured Alertmanager 0.33 payload)

- One event per alert **episode**, keyed `am:<fingerprint>:<startsAt>`:
  a `firing` delivery lands `started`, the later `resolved` delivery upserts
  the same row to `succeeded` with `duration = endsAt − startsAt`.
- `service`/`cluster`/`namespace` come from the alert's labels; `severity`
  becomes the rule fact `reason`. `env` is only set if a rule maps it
  (e.g. `match: {source: alertmanager, cluster: prod} → set: {env: prod}`).

## Use it

```bash
wtc around <alert-event-id> --window 30m     # id from `wtc log --kind alert`
wtc around 2026-07-14T13:41:34Z --window 1h  # or an explicit instant
```

## Incident forensics: from alert to cause

`wtc around` lists time-neighbors; `wtc blast` ranks them. Anchored on an
alert it scores every change in the preceding window as a suspect — a fixed,
documented heuristic (recency, same env as the hard signal, same service,
kind weight, a bump for failed/degraded changes), deterministic and never ML:

```bash
# 1. the alert fires — find it
wtc log --kind alert --since 2h

# 2. rank what likely caused it (id from step 1)
wtc blast <alert-event-id>                  # default --window 2h
#   SCORE  TIME      ENV   KIND    STATUS     SERVICE  TITLE            WHY
#   69     01:20:34  prod  deploy  succeeded  api      deploy api ...   30m before · same env (prod) · same service (api) · deploy

# 3. trace the top suspect end to end
wtc where <suspect-sha-or-tag>

# the reverse question — "did my deploy cause noise?"
wtc blast <deploy-event-id> --window 1h     # alerts that fired after it
```

An alert whose `env` was never inferred (check `wtc doctor`) disables the
same-env signal — the output says so; pass `--env` to restore it. A bare
RFC3339 instant works as the anchor too (`wtc blast 2026-07-18T12:00:00Z
--env prod`). The portal shows the same ranking in the alert drawer's
"Likely causes" panel.
