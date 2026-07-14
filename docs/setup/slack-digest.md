# Slack digest

Post the `handoff` digest (deploys/failures per env, infra changes,
rollbacks, unmapped counts, top actors, first-seen services) to Slack.

## One-off / cron-of-your-own

```bash
wtc handoff --since 24h --slack-webhook https://hooks.slack.com/services/XXX/YYY/ZZZ
```

Drop that in any scheduler (cron, a GitHub Actions `schedule:`, systemd timer).

## Built into serve

Let the daemon post on a fixed interval — add to `wtc.yaml`:

```yaml
digest:
  interval: 24h                       # 0 or unset disables
  window: 24h                         # how far back each digest looks (default = interval)
  slack_webhook: ${WTC_SLACK_WEBHOOK} # keep the URL in a secret, not the file
```

The first digest fires one interval after startup (never on restart), so
bouncing the pod doesn't spam the channel. The webhook URL is a secret —
provide it via `WTC_SLACK_WEBHOOK` (or `existingSecret` in the Helm chart),
never commit it.

Get an incoming-webhook URL from Slack: **Apps → Incoming Webhooks → Add to
a channel**.
