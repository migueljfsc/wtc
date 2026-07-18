# Wiring Argo CD notifications

Argo CD's notifications-controller pushes sync/health events to wtc from
inside the cluster — outbound only, works in fully private deployments.
Unlike Flux, Argo has **no fixed webhook schema**: the body is templated by
the operator, so wtc ships the template. Apply
[`argocd-notifications.yaml`](argocd-notifications.yaml) as-is — it IS the
contract `/ingest/argocd` parses. Verified against Argo CD v3.4.5 with
captured deliveries.

## 1. Config (wtc side)

```yaml
sources:
  argocd:
    webhook_secret: ${WTC_ARGOCD_WEBHOOK_SECRET}
    suppression_window: 10m   # drop resync re-notifications of the same (app,revision,phase|health)
```

Unset secret ⇒ `/ingest/argocd` fails closed (503).

### Scope: only track the apps you care about

Restrict which Applications enter the ledger with an allow/deny list matched on
**raw facts** (`namespace` = destNamespace, `object_name` = app name,
`object_kind` = `Application`, `project`). Empty ⇒ ingest every notification.

```yaml
sources:
  argocd:
    webhook_secret: ${WTC_ARGOCD_WEBHOOK_SECRET}
    suppression_window: 10m
    scope:
      allow:                          # empty ⇒ ingest everything
        - { project: "team-a" }       # fields within an entry are AND
        - { object_name: "payments-*" }
      deny:                           # deny wins over allow
        - { object_name: "*-preview" }
```

**Deny wins** over allow; empty `allow` allows all; empty `deny` denies none;
fields within an entry are AND, entries are OR; globs are `*`/`**`. Dropped
events are never stored (counted by `wtc_filtered_total{source="argocd"}`); a
bad pattern or an all-empty entry fails `wtc serve` at startup. It is a scope
declaration, not a query filter — Argo has no poller, so widening it later does
not recover past events.

Auth is a **static shared secret** sent verbatim as the `X-WTC-Token` header
and compared constant-time — Argo's notification templates cannot compute a
body HMAC like Flux's generic-hmac provider. Treat the value like a password;
it is deliberately excluded from capture-mode header dumps.

## 2. Cluster side

Apply [`argocd-notifications.yaml`](argocd-notifications.yaml) into the
namespace Argo CD runs in, after editing:

- the Secret's `wtc-token` (= `webhook_secret`)
- `service.webhook.wtc.url` — wtc's base URL reachable from that cluster
  (each template appends its own `path: /ingest/argocd`)

The shipped `subscriptions` block is a global default: every Application
reports to wtc with no per-app annotations. Delete it to opt in per-app
instead (annotation list at the top of the file).

Gotchas the shipped file already handles (all found by live-testing — keep
them if you adapt it):

- Argo CD's install manifest needs `kubectl apply --server-side` (the
  ApplicationSet CRD blows the client-side annotation limit).
- Webhook subscription recipients are the *name* half of
  `service.webhook.<name>` (`wtc`), not `webhook:wtc` — the wrong form fails
  with only a controller-log line, no delivery ever attempted.
- Trigger `when` clauses need `?.` after `.status` — `operationState` is
  absent until an app's first sync and the trigger silently never fires
  without the guard.
- `index .app.metadata.labels "env"` hard-errors on apps with no labels at
  all (untyped nil, not an empty map); the template guards with
  `{{if .app.metadata.labels}}`.

## 3. Rules that make Argo events useful

One Argo instance manages many clusters and its "cluster" is a destination
*server URL* — so env inference **never** uses cluster=env (unlike Flux).
The shipped tiers: explicit `env` app label > destination namespace >
app-name suffix. Unmatched apps land `env=""` and show up in `wtc doctor` —
wtc never guesses.

```yaml
rules:
  - match: { source: argocd }
    set:   { env: "{{ .EnvLabel }}" }    # no label → empty render → tier falls through
  - match: { source: argocd, namespace: prod }
    set:   { env: prod }
  - match: { source: argocd, object_name: "*-prod" }
    set:   { env: prod, service: '{{ trimSuffix .ObjectName "-prod" }}' }
  - match: { source: argocd }
    set:   { service: "{{ .ObjectName }}" }
```

(Repeat the namespace/suffix pairs for staging/dev — full block in SPEC §2.)
Also available to templates: `.Project`, `.DestServer`, `.SourcePath`.

## 4. Verify

```bash
argocd app sync <app>        # or the kubectl fallback below
wtc log --env <env> --kind deploy --since 10m
```

No `argocd` CLI session? Trigger a sync with kubectl alone:

```bash
kubectl -n argocd patch application <app> --type merge \
  -p '{"operation":{"sync":{"revision":"HEAD"}}}'
```

## 5. Troubleshooting

- **First diagnostic, always** — the notifications-controller log says which
  triggers fired, which deliveries were attempted, and why templating failed:

  ```bash
  kubectl -n argocd logs deploy/argocd-notifications-controller
  ```

- **"It notified once, then never again":** the controller records sent
  notifications in a `notified.notifications.argoproj.io` annotation on the
  Application (keyed by trigger+condition hash). Clear it to force re-sending:

  ```bash
  kubectl -n argocd annotate application <app> notified.notifications.argoproj.io-
  ```

- **No delivery attempted at all:** check the recipient format (`wtc`, not
  `webhook:wtc`) and the trigger `when` guards — both fail with only a log
  line (see the gotchas in §2).

## Behavior notes (from captured payloads)

- The sync `revision` is the manifest-repo git sha and lands in `ref` —
  `wtc where <sha>` reports Argo-applied envs exactly like Flux reconciles,
  in the same picture.
- `operationPhase` mapping: `Running` → started, `Succeeded` → succeeded,
  `Error` **and** `Failed` → failed. A sync that never starts applying (bad
  path, unresolvable revision) reports `Error`, not `Failed` — observed live.
- `healthStatus: Degraded` wins over the phase and upserts the sync
  operation's own row to status `degraded` — the notification carries the
  *previous* sync's phase and `startedAt` (which keys it to that row), so the
  degradation is stamped with receipt time. **Recovery on the SAME revision
  is invisible by design**: `degraded` outranks the terminal statuses, so
  nothing lower can overwrite that row — expect recovery to show as the next
  operation's row (a retry or a new revision), and treat a lingering
  `degraded` row as "this deploy went bad", not "still bad now".
- One row per sync **operation** (`app`+`revision`+`startedAt`): a Running →
  Succeeded/Error transition updates its row in place, while a *retry* of the
  same revision is a new operation → a new row — the ledger shows the failed
  attempt AND the successful retry.
- Identity fields (`env`, `service`, `actor`, …) on a completed row never
  regress — rows are historical facts (non-empty-wins merge, SPEC §1).
  Post-completion changes appear as new rows via the per-operation key.
- Argo *can* re-notify on resyncs of an unchanged revision (fresh operation
  timestamps defeat its rendered-body hash dedup — observed live), and the
  suppression window sheds those; even disabled, the dedup upsert stores one
  row per operation. But the inverse limitation also holds: the controller's
  `notified.notifications.argoproj.io` annotation is keyed by
  trigger+condition hash, so a fast resync whose `Running` phase the
  controller never observed may send **nothing at all** — wtc can miss a
  legitimate same-revision resync entirely. That is an Argo-side limitation,
  not a wtc dedup artifact.
- `triggeredBy` is only populated for syncs initiated through the Argo
  API/UI with a logged-in user; controller- or kubectl-driven syncs report
  actor `argocd`.
- Multi-source apps (2.6+) emit `revisions[]`; captured fixtures cover
  single-source only — the array is parsed and kept in the payload but the
  primary `revision` drives the where join.
