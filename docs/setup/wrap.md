# wtc wrap — record manual and CI-driven changes

Wrap any command to record its lifecycle: a `started` event before, a
`succeeded`/`failed` upsert onto the same row after, with duration and exit
code. Stdio is inherited and the exit code passes through — wrap is
transparent to scripts and CI. **A dead wtc server prints a warning and the
command still runs**; wtc never blocks operations.

```bash
# feature-branch helm install (service/namespace/chart/tag sniffed)
wtc wrap --env pr-123 -- helm upgrade pr-123 ./chart -n pr-123 --set image.tag=sha-abc1234

# terraform in CI (kind=infra_change; -json stream counted, plans never stored)
wtc wrap --env prod -- terraform apply -auto-approve -json

# anything else lands as source=generic kind=manual
wtc wrap --env prod --title "manual db failover" -- ./scripts/failover.sh
```

Client config comes from `WTC_SERVER` and `WTC_API_TOKEN` — in CI, set both
as secrets/vars on the job.

## What the sniffers infer

| command | kind | service | namespace | artifact | extras |
|---|---|---|---|---|---|
| `helm upgrade\|install <release> <chart>` | deploy | release | `-n`/`--namespace` | chart | `--set image.tag=` → payload + artifacts |
| `terraform\|tofu apply\|destroy` | infra_change | — | — | — | with `-json`: `(+a ~c -d)` counts in title |
| anything else | manual | — | — | — | — |

`--service`, `--env`, `--title` override anything sniffed. Titles and
payloads pass the server-side redaction deny-list, so a `--set password=...`
in the command line is scrubbed before storage.
