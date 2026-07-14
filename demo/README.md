# wtc demo services

Three dummy microservices (`api`, `web`, `worker`) whose only job is to
generate real change events for wtc: CI builds, GHCR image pushes,
commitizen-driven versions, kustomize overlays per env, and Flux reconciles
into a local kind cluster posing as three clusters (`dev`/`staging`/`prod`
namespaces, per-env Alerts faking cluster metadata).

Each service is deliberately shaped like the operator's production stack:
its own version lifecycle (`.cz.yaml`, tag `demo-<svc>-vX.Y.Z`), its own
`infrastructure/base` + `overlays/<env>` tree, and images tagged both
`sha-<sha7>` and `<version>-<sha7>` — the two default `tag_patterns`.

## Flows this generates

- **build**: push touching `demo/<svc>/**` → `demo` workflow → GHCR push
  (workflow_run events; service inferred from the workflow fact)
- **release**: merge to main → cz bump commit + tag `[skip ci]` → versioned
  image
- **promote**: PR editing `overlays/<env>/kustomization.yaml` `newTag:` →
  merge → Flux reconciles that env (merge + deploy events; the tag↔revision
  join `wtc where` traverses)

## Cluster wiring

`demo/flux/` holds the kind-cluster objects: namespaces, GitRepositories,
one Kustomization per (service, env), and three Alerts with
`eventMetadata.cluster: <env>`. Apply after `flux install`:

```bash
kubectl apply -f demo/flux/
```

GHCR packages must be public (or add pull secrets to the three namespaces).

This tree is a test bed, not product code: the demo services are separate Go
modules, invisible to the root module's build/test/lint.
