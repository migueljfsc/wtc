# Changesets — one commit, one change

A single code change fans out into many ledger rows: a build, a merge, and a
deploy per environment — and each env's deploy carries a *different* manifests
revision. `wtc changes` collapses all of them back into one **changeset**,
keyed by the app commit sha, so you see the change once with every env it
reached.

```bash
wtc changes --since 7d
wtc changes --since 24h --json
```

```
CHANGE   SERVICES  ENVS              STATUS    LATEST            TITLE
8f82946  api       dev,staging,prod  deployed  2026-07-21 11:51  feat(api): faster catalog lookup
5ab1276  web       dev,staging       deployed  2026-07-21 10:14  fix(web): retry flaky upload
```

## How grouping works

wtc resolves each event's app commit sha the same way [`wtc where`](../SPEC.md)
does — a build's git ref, a merge's image-bump tag, or a deploy's artifact tag
(via `tag_patterns`) — and folds events sharing that sha into one changeset. A
deploy also joins by matching an intent's manifests revision, so per-env
overlay bumps still land on the right change. Events with no resolvable app sha
(flux reconciles with no image artifact, alerts) aren't part of a changeset.

Each changeset reports the services touched, the envs a succeeded deploy
reached, the owning team(s), whether anything failed, and the event count.
Expand the full journey for one change with `wtc where <sha>`.

## Surfaces

- **CLI:** `wtc changes [--since 7d] [--until …] [--json]`.
- **API:** `GET /api/v1/changesets?since=…&until=…`.
- **Portal:** a **Changes** view — one card per change; the sha links to its
  full `where` journey.
