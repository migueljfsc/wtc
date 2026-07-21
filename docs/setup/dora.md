# DORA — change-failure rate & MTTR

wtc turns the change ledger into deploy-quality metrics: **change-failure rate**
and **MTTR**, overall and grouped by environment and owning team. (Deploy
frequency is on the dashboard already; lead time isn't computed yet — it needs
the tag↔sha join `wtc where` performs.)

```bash
wtc dora --since 30d
wtc dora --since 90d --window 2h --json
```

## What it measures

**Change-failure rate** — of the terminal deploys (succeeded + failed) in the
window, the fraction that "failed": the deploy failed outright, **or** an
`alert` or `rollback` landed in the **same env** within `--window` after it
(default `1h`). Env is the correlation key — alerts often carry no clean
service, so matching on env keeps the signal robust. Tune the window to your
alerting latency.

**MTTR** — the mean firing→resolved duration of resolved alerts in the window.
This comes straight from Alertmanager episodes (`endsAt − startsAt`), so it's
only populated once [Alertmanager ingest](alertmanager.md) is wired.

Both are reported three ways: **overall**, **by env**, and **by owner** (the
team from your [service catalog](ownership.md)). A group appears only when it
has a non-empty key, so unmapped env/owner rows still count toward the overall
totals but don't create noise groups.

## Prerequisites

- **MTTR and alert-driven failures** need [Alertmanager ingest](alertmanager.md).
  Without it, change-failure rate still counts failed deploys and rollbacks.
- **By-team** grouping needs [ownership](ownership.md) configured.

## Surfaces

- **CLI:** `wtc dora [--since 30d] [--until …] [--window 60m] [--json]`.
- **API:** `GET /api/v1/dora?since=…&until=…&window=60m`.
- **Portal:** a *Delivery quality (DORA)* section on the dashboard — change-
  failure rate, MTTR, incident count, and a per-env breakdown.

The failure attribution and MTTR are a documented, deterministic computation —
no ML, no external service.
