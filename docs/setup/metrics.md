# Prometheus metrics

`wtc serve` exposes `/metrics` in the Prometheus exposition format. The endpoint
is **bearer-authed with `auth.api_tokens`** — the same tokens as `/api/*` —
because wtc may be public (see the exposure posture in `github-webhook.md`) and
`/metrics` leaks source names and activity levels. For in-cluster scrapes where
handing Prometheus an api_token would be over-privileged (it also grants
`/api/*`, including config writes), run a **separate unauthenticated listener**
instead (below).

## What's exported

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `wtc_ingested_total` | counter | `source` | Events stored as **new** rows |
| `wtc_deduped_total` | counter | `source` | Deliveries merged onto an existing row (poller sweeps, redeliveries, status updates) |
| `wtc_suppressed_total` | counter | `source` | Events dropped inside a suppression window (flux/argocd reconcile spam) |
| `wtc_mapping_errors_total` | counter | `source` | Mapping-webhook template failures (delivery rejected, sender can retry) |
| `wtc_poll_last_success_timestamp_seconds` | gauge | `source`, `repo`, `resource` | Unix time of the last successful poll; lag = `time() - <gauge>` |
| `wtc_db_size_bytes` | gauge | `backend` | DB size, sampled per scrape (`pragma` on sqlite, `pg_database_size` on postgres) |
| `wtc_http_request_duration_seconds` | histogram | `path`, `method`, `status` | Request latency; `path` is the **route pattern** (`/api/v1/where/{ref}`), never the raw URL |
| `wtc_sse_connections` | gauge | — | Open `/api/stream` SSE connections |

Standard `go_*` and `process_*` collectors are registered too.

> `wtc_ingested_total` + `wtc_deduped_total` partition every accepted delivery:
> the first is real change flow, the second is replay/redelivery noise. Both are
> incremented in the single-writer path, so they stay complete across every
> ingest source.

## Scrape config (bearer-authed, main port)

```yaml
scrape_configs:
  - job_name: wtc
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials: <an api_token from auth.api_tokens>
    static_configs:
      - targets: ["wtc:8484"]
```

## Separate unauthenticated listener (in-cluster only)

Set `metrics.listen` to open a second listener that serves **only** `/metrics`,
with no auth. Keep it in-cluster behind a NetworkPolicy — never route it through
the ingress.

```yaml
metrics:
  listen: ":9091"     # empty (default) = no extra listener; /metrics stays on :8484 bearer-authed
```

Env override: `WTC_METRICS_LISTEN=:9091`. A configured listener that cannot bind
is fatal, like the main one — silently running without metrics defeats the point
of asking for them.

## Helm

```yaml
metrics:
  port: 9091                   # shipped default. <n> = unauth listener on <n>; 0 = main port only (bearer).
  serviceMonitor:
    enabled: false
    interval: 30s
    labels: {}                 # e.g. {release: kube-prometheus-stack} for Prometheus discovery
  scrapeAnnotations: false     # prometheus.io/* pod annotations (only with metrics.port set)
```

Two scrape models:

1. **`metrics.port: 9091` (shipped default)** — wtc opens the unauthenticated
   listener, the chart adds a `metrics` container/Service port (ClusterIP, so
   in-cluster only), and the ServiceMonitor scrapes it without credentials.
   Annotation-based discovery (`scrapeAnnotations: true`) is available in this
   model only — annotation scrapes cannot carry bearer auth.
2. **`metrics.port: 0`** — no extra listener; scrape the main port. The
   ServiceMonitor reads the bearer token from `existingSecret` key
   `WTC_API_TOKEN`, so that key is **required** when `serviceMonitor.enabled` in
   this model (the chart errors otherwise). An api_token also grants `/api/*`;
   prefer the default if you don't want Prometheus holding that.

The ServiceMonitor selector matches only the API Service; the portal `-ui`
Service carries a different name label and is never scraped.

## Example alerts

```yaml
groups:
  - name: wtc
    rules:
      # A source that normally reports has gone silent.
      - alert: WtcSourceSilent
        expr: time() - max by (source) (wtc_poll_last_success_timestamp_seconds) > 1800
        for: 10m
        annotations:
          summary: "wtc poller {{ $labels.source }} has not succeeded in 30m"

      # Any mapping-webhook template failure — deliveries are being rejected.
      - alert: WtcMappingErrors
        expr: increase(wtc_mapping_errors_total[15m]) > 0
        annotations:
          summary: "wtc mapping webhook {{ $labels.source }} is failing to render"

      # Ledger is not growing — check the ingest path is alive.
      - alert: WtcNoIngest
        expr: sum(increase(wtc_ingested_total[1h])) == 0
        for: 2h
        annotations:
          summary: "wtc ingested no new events in the last hour"
```

Tune `WtcSourceSilent` to your busiest source's cadence; a genuinely idle source
(no changes to report) will also trip it, so scope the `source` label if needed.
