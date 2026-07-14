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
