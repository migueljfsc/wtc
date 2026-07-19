# Notifications — push, don't just store (P21)

wtc can push change events out as they land: one subscription engine, three
sink types, plus a pull-based Atom feed. Notifications fire on **new events**
and on **status transitions** (a deploy's row upserting `started → failed`
fires a `status: failed` subscription), and never on redeliveries that change
nothing — idempotency comes from the same rank-guarded upsert that keeps the
ledger to one row per logical change.

## Subscriptions

```yaml
notifications:
  # Slack message for every prod deploy or rollback
  - name: prod-deploys
    match:
      env: prod
      kind: deploy            # globs: * (one segment), ** (any depth)
    sink:
      type: slack
      url: ${WTC_NOTIFY_SLACK}   # incoming-webhook URL — a secret, use ${VAR}

  # Page-worthy: anything failing anywhere
  - name: failures
    match:
      status: failed
    sink:
      type: webhook
      url: https://automation.internal/hooks/wtc
      token: ${WTC_NOTIFY_WEBHOOK_TOKEN}   # optional; sent as Authorization: Bearer

  # Overlay deploys on Grafana dashboards
  - name: grafana-overlay
    match:
      kind: deploy
    sink:
      type: grafana-annotation
      url: https://grafana.internal        # base URL; wtc appends /api/annotations
      token: ${WTC_GRAFANA_SA_TOKEN}       # service-account token, role Editor
      tags: [deploys]                      # extra tags; wtc always adds
                                           # "wtc", the kind, env:<env>, service:<svc>
```

- `match` fields (`env`, `service`, `repo`, `kind`, `status`) use the same
  glob dialect as `rules[].match`. Empty fields are unconstrained; an empty
  match subscribes to everything. Matching runs against the **post-merge
  row**, so a lifecycle completion that omits env still matches `env: prod`
  via what the ledger kept.
- Config is validated at startup: an unknown sink type or missing URL/token
  fails `wtc serve`, never a delivery.
- Sink URLs and tokens are secrets: masked in `wtc config` and `/api/config`.

## Sinks

**slack** — posts one mrkdwn line per event to an incoming webhook
(`api.slack.com` → "Incoming Webhooks"). Same webhook kind as the digest;
use a channel like `#deploys`.

**webhook** — POSTs JSON to any endpoint:

```json
{
  "notification": "failures",
  "transition": true,
  "event": { "id": "01J…", "env": "prod", "kind": "deploy", "status": "failed", "...": "…" }
}
```

`transition: true` means a status change on an existing row (vs first
sighting). `event` is the normalized row without the raw payload; fetch
`/api/events` with the id if you need more.

**grafana-annotation** — POSTs to Grafana's `POST /api/annotations` so "what
changed" overlays dashboards (round-trip captured against Grafana 11.3.0;
fixtures in `testdata/grafana/`). Setup: Administration → Service accounts →
add account (role **Editor**) → add token. On a dashboard, add an annotation
query for tag `wtc` (or `env:prod`, or your extra tags) — deploys appear as
vertical markers.

## Delivery semantics

Best-effort, at-least-once: a bounded in-memory queue off the ingest path
(ingest is never blocked or slowed), 4 attempts with backoff (1s/4s/16s),
then the delivery is dropped and logged. Watch:

- `wtc_notify_sent_total{notification,sink}`
- `wtc_notify_failed_total` — failed attempts; alert on sustained rate
- `wtc_notify_dropped_total{reason="queue_full"|"retries_exhausted"}` — any
  increase is a lost notification

Queued deliveries are lost on restart (no durable outbox in v1). If a
notification is compliance-critical, consume the Atom feed or `/api/events`
instead — the ledger itself is the durable record.

## Atom feed (pull)

`GET /feed` serves the latest events as Atom for feed readers — no sink
config needed. Filters: `env`, `service`, `repo`, `kind`, `status`, `source`,
`limit` (default 50, max 200). Auth is the regular api_token, accepted as a
`?token=` query parameter because feed readers can't set headers:

```
https://wtc.internal/feed?env=prod&kind=deploy&token=<api_token>
```

Entry ids carry the status (`urn:wtc:<id>:<status>`), so a status transition
shows up as a new entry instead of being swallowed by reader-side dedup. The
token ends up in your reader's config — rotate `auth.api_tokens` to revoke.
