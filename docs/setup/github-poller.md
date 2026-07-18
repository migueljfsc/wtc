# Wiring GitHub via the API poller (primary path)

The poller pulls workflow runs, merged PRs, and default-branch commits for
each configured repo. It needs only **outbound** HTTPS to api.github.com — no
public endpoint, no webhooks. It is idempotent (stable dedup keys), so it also
backfills anything a webhook deployment missed.

## 1. Token

Fine-grained PAT scoped to the repos you want watched, **read-only**:

- Actions (workflow runs)
- Contents (commits)
- Pull requests
- Metadata (implicit)

Classic PAT alternative: `repo` scope. Rate budget: one repo costs 3 requests
per poll — at 60s that's 180/h/repo against a 5,000/h limit.

## 2. Config

```yaml
sources:
  github:
    api_token: ${WTC_GH_API_TOKEN}
    poll_interval: 60s        # 0 disables the poller
    repos:
      - your-org/app-api      # exact
      - your-org/svc-*        # glob: every repo matching the prefix
      - "*/deploy-*"          # glob: any org the token can see
    infra_path: infrastructure/
```

`repos` entries may be **globs** (`*` = one path segment, `**` = any depth —
the same dialect as `rules:` matches). Globs are resolved against the repos
the token can access, **re-discovered every sweep**, so a new repo matching a
pattern is picked up without a restart. An empty `repos` list still means
"everything the token can see"; exact entries are polled as-is. A pattern
that doesn't compile fails startup.

Export `WTC_GH_API_TOKEN` in the serve environment (Kubernetes: a Secret →
env var). Never write the token into the file.

## 3. Verify

```bash
wtc serve --config wtc.yaml &
sleep 90                       # one poll interval + margin
wtc log --since 24h            # builds/merges/pushes appear
wtc doctor                     # per-source health + poller watermarks
```

First run backfills 24h. Each poll re-reads a 1h overlap window so runs that
were in progress get their terminal status — duplicates are impossible by
design (`dedup_key` upsert).

## Notes

- Build/push events land with `env=""` — REST payloads carry no changed-file
  lists, and wtc never guesses (see `wtc doctor`). Env inference comes from
  Flux events (cluster→env) and PR-diff enrichment in later phases.
- A repo with zero recent activity stores nothing; the watermark only
  advances when events are stored.
