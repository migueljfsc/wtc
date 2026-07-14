# Optional: report builds from CI via /ingest/generic

Not needed when image tags embed the git sha (the default `tag_patterns`
resolve them). For pipelines whose tags don't, add one step after the image
push so `wtc where` can join builds to deploys through `artifacts`:

```yaml
- name: report build to wtc
  if: always()
  run: |
    curl -sf -X POST "$WTC_SERVER/ingest/generic" \
      -H "Authorization: Bearer $WTC_API_TOKEN" \
      -H "Content-Type: application/json" \
      -d @- <<EOF
    {
      "kind": "build",
      "service": "${{ github.event.repository.name }}",
      "ref": "$GITHUB_SHA",
      "status": "${{ job.status == 'success' && 'succeeded' || 'failed' }}",
      "title": "${{ github.workflow }} #${{ github.run_number }}",
      "url": "${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}",
      "dedup_key": "ci:${{ github.repository }}:${{ github.run_id }}:${{ github.run_attempt }}",
      "artifacts": ["registry.example.com/app:${{ steps.meta.outputs.version }}"]
    }
    EOF
```

Requires network reachability from the runner to wtc — for private
deployments that usually means self-hosted runners. The `dedup_key` derives
from run id + attempt, so retries and redeliveries never duplicate.
