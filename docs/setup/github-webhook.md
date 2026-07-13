# Wiring GitHub webhooks (optional — needs a public endpoint)

> **Status:** `/ingest/github` currently authenticates (HMAC) and captures
> deliveries; webhook-envelope normalization lands once real webhook fixtures
> are frozen (fixture-first rule — see docs/PLAN.md P1 note). If wtc is not
> reachable from the internet, skip this page: the poller covers everything.

## 1. Secret

```yaml
sources:
  github:
    webhook_secret: ${WTC_GH_WEBHOOK_SECRET}
```

Unset ⇒ the endpoint fails closed (503 for every delivery).

## 2. GitHub side (org or repo → Settings → Webhooks)

- Payload URL: `https://wtc.example.com/ingest/github`
- Content type: `application/json`
- Secret: same value as `WTC_GH_WEBHOOK_SECRET`
- Events: `Workflow runs`, `Pushes`, `Pull requests`

## 3. Verify

Send a test delivery from GitHub's webhook UI; the serve log shows
`github delivery` with the event type, and GitHub's "Recent Deliveries" shows
`202`. A `401` means the secret doesn't match; wtc compares
`X-Hub-Signature-256` in constant time against the raw body.

## Webhooks + poller together

Safe and recommended when a public endpoint exists: webhooks give latency,
the poller heals gaps (serve downtime, delivery loss). Dedup keys derive from
run/PR/commit identity, so both paths land on the same rows.
