# Wiring a mapping webhook (long-tail ingest)

Any tool that can POST JSON becomes a wtc source through **configuration, not
code**. You declare a source under `sources.webhooks[]`: how to authenticate it,
how to map its payload onto the wtc Event, and a stable `dedup_key`. Mapped
events flow through the same pipeline as every other source (rules → redaction →
status-rank upsert), so lifecycle transitions and env/service inference work
exactly as they do for GitHub or Flux.

Endpoint: `POST /ingest/webhook/<name>` where `<name>` is the source's `name`.

Two shipped **presets** — `grafana` and `jenkins` — give you a tested mapping
out of the box; you only supply the name and your own secret. For everything
else, capture the payload once and write the mapping.

## Presets

```yaml
sources:
  webhooks:
    - name: grafana                 # POST /ingest/webhook/grafana
      preset: grafana
      auth:
        token: ${WTC_GRAFANA_TOKEN} # static shared secret you choose
        header: Authorization       # header Grafana sends it in (default X-WTC-Token)

    - name: jenkins                 # POST /ingest/webhook/jenkins
      preset: jenkins
      auth:
        token: ${WTC_JENKINS_TOKEN}
```

A preset supplies the field/dedup/facts templates; **you always supply `name`
and `auth`** (a shared secret is per-installation and is never baked into a
preset). Override any preset field by setting it alongside `preset:` — your
value wins, the rest is inherited.

### Grafana (contact point → webhook)

Grafana **Alerting → Contact points → Add**, type *Webhook*:

- **URL** `https://wtc.example.com/ingest/webhook/grafana`
- **Authorization Header** — set the header + value matching your `auth` above
  (e.g. header `Authorization`, value your token).

One Event per delivery, keyed to the alert **episode**
(`grafana:<fingerprint>:<startsAt>`): a *firing* delivery lands `started`, the
later *resolved* delivery upserts the same row to `succeeded`. `kind=alert`, so
it is a correlation anchor for `wtc around`, never part of `diff`/`where`.
`env` comes from a `commonLabels.env` label if present; `cluster`, `namespace`
and `severity` (as the rule fact `reason`) come from `commonLabels`.

### Jenkins (Notification Plugin)

Install the **Notification Plugin**, then on a job add **Job Notifications →
Add Endpoint**: Format *JSON*, Protocol *HTTP*, URL
`https://wtc.example.com/ingest/webhook/jenkins`, and add a header carrying your
token. (Jenkins' outbound requests are SSRF-guarded — the target must be a
routable, non-private address.)

One row per build keyed `jenkins:<job>:<number>`: the `STARTED` phase lands
`started`, `COMPLETED`/`FINALIZED` upsert the same row to `succeeded`/`failed`.
`ref` is the git commit (from the job's SCM), which makes the build joinable by
`wtc where <sha>`; `branch` and the job name feed the rules engine as facts.

## Authoring a mapping for a novel tool

The workflow is **capture-first** — never guess field names:

1. Point the tool at wtc with capture mode on:
   `wtc serve --capture-dir ./cap` (and a placeholder webhook so auth passes).
   The raw body is written to `./cap/webhook/<name>-*.json`.
2. Read the captured JSON, then write the mapping. Templates are Go
   `text/template` over the parsed body — the **same engine and funcs**
   (`trimPrefix`, `trimSuffix`, `lower`, `regexReplace`, plus `default`) that
   `rules[].set` uses. Access fields with `.a.b`, arrays with `index`.

```yaml
sources:
  webhooks:
    - name: harbor
      auth:
        token: ${WTC_HARBOR_TOKEN}
      dedup_key: 'harbor:{{ .event_data.repository.name }}:{{ (index .event_data.resources 0).tag }}'
      mapping:
        kind: build
        status: succeeded
        title: 'Harbor push {{ .event_data.repository.name }}'
        ref: '{{ (index .event_data.resources 0).digest }}'
        url: '{{ (index .event_data.resources 0).resource_url }}'
      facts:
        service: '{{ .event_data.repository.name }}'
```

### Auth: static token or HMAC

```yaml
      # static shared secret in a header (constant-time compared):
      auth: { token: ${SECRET}, header: X-My-Token }   # default header X-WTC-Token

      # OR the sender signs the body (hex HMAC):
      auth:
        hmac:
          secret: ${SECRET}
          header: X-Signature       # header carrying the signature
          algo:   sha256            # sha256 (default) | sha512 | sha1
          prefix: "sha256="         # stripped from the header value if present
```

Exactly one of `token`/`hmac` is required — a webhook with neither fails closed
(rejects every request).

### Mappable fields

`kind` (required, must be a valid wtc kind), `title` (required), `status`
(default `unknown`), `env`, `cluster`, `namespace`, `service`, `actor`, `ref`,
`artifact`, `url`, `ts` (RFC3339; default = ingest time), `duration_ms`.
`dedup_key` (required) is a template too.

`facts` populate the rules engine (`repo`, `branch`, `event`, `workflow`,
`actor`, `cluster`, `namespace`, `object_kind`, `object_name`, `reason`). A
field set directly in `mapping` wins; anything left empty there can be filled by
a rule that matches on these facts (`source: <name>` is always available).

## The `dedup_key` footgun — and how doctor guards it

Ingestion is at-least-once, so idempotency depends entirely on a **stable**
`dedup_key` derived from source-side identifiers (a run id, a fingerprint, an
object+revision) — **never** something that changes per delivery (a receive
timestamp, a random id). An unstable key silently turns retries into duplicate
rows.

`wtc doctor` guards this two ways:

- **Churn heuristic** — many rows sharing the same title/kind/status that landed
  seconds apart under *distinct* dedup keys are flagged as
  `webhook dedup_key churn`. That is the fingerprint of a key that should have
  collapsed onto one row but didn't.
- **Mapping errors** — a template that fails to render, or renders an empty
  `dedup_key`/`title`, rejects the delivery with `422` (the sender can retry)
  and is counted under `webhook mapping errors`. A mapping error is surfaced,
  never guessed.

## `/ingest/generic` vs a mapping webhook

Use **`/ingest/generic`** (or `wtc record`/`wtc wrap`) when *you own the sender*
and can post wtc's own schema — no mapping needed. Use a **mapping webhook**
when a third-party tool emits its own JSON shape you don't control.
