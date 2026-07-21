# Ownership — stamp each change with its owning team

wtc can attach an **owner** (owning team) to every change, inferred at ingest
from the event's `service` (with the source `repo` as a fallback). Once wired,
owner is a first-class dimension: filter by it (`wtc log --owner`, portal
facet), route notifications to it (`match.owner`), and see which services aren't
yet in your catalog (`wtc doctor`).

Owner is stored on the row **as of the change**, so historical events keep the
owner they had when they happened — reassigning a team later doesn't rewrite the
past.

## Configure a catalog

Point wtc at one or more catalog files. Sources are scanned in a fixed priority
order regardless of listing order — **backstage > datadog > services > codeowners**
— and the first non-empty owner for a service wins:

```yaml
catalog:
  sources:
    - type: backstage           # Backstage catalog-info.yaml (spec.owner)
      path: ./catalog/**/catalog-info.yaml
    - type: datadog             # Datadog service catalog (team)
      path: ./service.datadog.yaml
    - type: services            # wtc's own simple format
      path: ./services.yaml
    - type: codeowners          # a repo's default (*) owner
      path: ./CODEOWNERS
      repo: my-org/my-service
```

`path` may be a glob (`**` matches any depth). A literal path that doesn't
exist, or a glob that matches nothing, fails startup — a mis-scoped catalog
must not pass silently. The files are read by `wtc serve` at boot; the CLI
subcommands never need them.

wtc runs as a single binary, so the catalog files must be reachable from the
serve process — mount them into the pod (ConfigMap/volume) or bake them into
the image, and give `path` the in-container location.

### Formats

**Backstage** — every `kind: Component` contributes `metadata.name → spec.owner`.
An entity-ref prefix is stripped, so `group:platform` and `platform` are the
same owner.

```yaml
apiVersion: backstage.io/v1alpha1
kind: Component
metadata:
  name: api
spec:
  owner: group:platform
```

**Datadog service catalog** — v2 (`dd-service` + `team`) and v3 (`kind: service`,
`metadata.name`, `team`) are both read.

```yaml
schema-version: v2.2
dd-service: api
team: platform
```

**services.yaml** — wtc's own minimal format. `owner`/`team` and
`service`/`name` are accepted; an optional `repo` also seeds the repo fallback.

```yaml
services:
  - service: api
    owner: platform
    repo: my-org/api
```

**CODEOWNERS** — supplies a repo's default team from its broadest (`*`) rule.
Because a CODEOWNERS file names paths, not services, each `codeowners` source
must declare the `repo` it governs. The owner is keyed by repo and used only
when no service match is found.

```
*        @my-org/platform
/docs    @my-org/docs
```

## Use it

- **CLI:** `wtc log --owner platform --since 24h`
- **Portal:** an **owner** facet in the timeline filter bar; owner also shows in
  the event drawer.
- **Notifications:** add `owner` to a subscription match (rules-engine glob
  dialect):

  ```yaml
  notifications:
    - match: { owner: platform, status: failed }
      sink: { type: slack, url: ${SLACK_WEBHOOK} }
  ```

- **Gaps:** `wtc doctor` lists `unowned_services` — services that appear without
  an owner while a catalog is in use. Add them to the catalog. (When no catalog
  is configured, this is silent — every service is unowned by definition.)
