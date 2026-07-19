# Wiring GitHub webhooks (optional — needs a public endpoint)

`/ingest/github` authenticates deliveries (HMAC) and normalizes `workflow_run`,
`push`, and `pull_request` events into the same Events + dedup keys the poller
produces. Webhook and poller are **peer modes**: run the webhook for
latency, the poller as the loss-recovery sweeper, or either alone.

If wtc is not reachable from the internet, skip this page — the
[poller](github-poller.md) covers everything with outbound-only HTTPS.

## 1. Secret

```yaml
sources:
  github:
    webhook_secret: ${WTC_GH_WEBHOOK_SECRET}
```

Unset ⇒ the endpoint fails closed (503 for every delivery). Any strong random
string; install the **same value** on both sides. wtc verifies GitHub's
`X-Hub-Signature-256` (HMAC-SHA256 over the raw body) in constant time.

## 2. GitHub side (org or repo → Settings → Webhooks)

- Payload URL: `https://wtc.example.com/ingest/github`
- Content type: `application/json`
- Secret: same value as `WTC_GH_WEBHOOK_SECRET`
- Events: **Workflow runs**, **Pushes**, **Pull requests**

## 3. Verify

Trigger a build or push (or use GitHub's "Redeliver" on a past delivery). The
serve log shows `github delivery` with `ingested`/`deduped` counts, GitHub's
**Recent Deliveries** shows `201`/`200`, and `wtc log --since 1h` shows the
build/merge/push. Response codes:

- `201` — at least one new row landed.
- `200` — every event deduped (a redelivery, or the poller already had it).
- `202` — nothing to ingest (a `ping`, a non-merge PR action, a
  closed-without-merge PR).
- `401` — bad/missing signature. `503` — `webhook_secret` unset.

## What lands

| GitHub event | Rows | Notes |
|---|---|---|
| `workflow_run` | one build per run | status upserts across `requested`→`in_progress`→`completed`; one row per run id + attempt |
| `push` | one push per commit | commit file lists drive path env inference; a very large push is capped by GitHub, and the poller sweeps the remainder |
| `pull_request` | one merge per **merged** PR | `opened`/`synchronize`/closed-without-merge are acknowledged and dropped |

Revert PRs land as `kind=rollback`. Dedup keys: `gh:run:<repo>:<id>:<attempt>`,
`gh:push:<repo>:<sha>`, `gh:pr:<repo>:<number>:merged`.

## Webhook + poller together

Recommended when a public endpoint exists: the webhook gives latency (a merge
shows up in seconds, not a poll interval), the poller heals gaps (serve
downtime, dropped deliveries). Both derive dedup keys from run/PR/commit
identity, so a run seen by both lands on **one** row — a redelivery or a poller
sweep of the same run is idempotent by design.

See [onboarding.md](onboarding.md#ingest-posture) for choosing the posture
(private → poller-primary; public → webhook + poller sweeper).
