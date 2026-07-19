# wtc demo services

Three dummy microservices (`api`, `web`, `worker`) whose only job is to
generate real change events for wtc, continuously. This is the live test bed
that exercises wtc end-to-end: a real promotion PR traced by `wtc where` from
build → merge → reconcile across three envs.

Each service is shaped like a production microservice: its own Go module
(invisible to the root build), its own commitizen version lifecycle
(`.cz.yaml`, tags `demo-<svc>-vX.Y.Z`), its own
`infrastructure/base` + `overlays/{dev,staging,prod}` kustomize tree, and
images on GHCR tagged `sha-<sha7>` **and** `<version>-<sha7>` — the two
default `tag_patterns` wtc resolves.

## The flows this generates

| Flow | Trigger | Events wtc ingests |
|---|---|---|
| build + release | push touching `demo/<svc>/**` (manifests excluded) | workflow_run (service-attributed), bump commit, GHCR image |
| background noise | staggered crons + coin flip | occasional rebuilds |
| promote | PR editing `overlays/<env>/kustomization.yaml` `newTag:`, merged | enriched merge (env inferred from paths, image_bumps payload) |
| deploy | Flux reconciles the wtc repo every 1m | deploy events per fake cluster |

## Cluster wiring (`demo/flux/`)

A local kind cluster (`kind create cluster --name wtc-dev` + `flux install`)
poses as three clusters: namespaces `dev`/`staging`/`prod`, one Flux
Kustomization per (service, env), and three Alerts whose
`eventMetadata.cluster` fakes per-cluster identity. Apply with:

```bash
kubectl --context kind-wtc-dev apply -f demo/flux/
```

(Always pin `--context` — a work-cluster kubeconfig context nearly ate a
namespace once.)

## CI requirements

The demo workflows need the **`CZ_TOKEN` repo secret** (fine-grained PAT,
this repo only, Contents: read/write) — see
[.github/workflows/README.md](../.github/workflows/README.md) for why the
default `GITHUB_TOKEN` cannot push version bumps here, and why the PAT must
be the *checkout* credential (http.extraheader gotcha).

## Known monorepo caveats (deliberate — the real operator stack is repo-per-service)

- Every commit is one manifests revision for *all* envs, so revision-level
  `where` shows all envs applied; artifact-level truth needs the tag joins.
- commitizen can't filter commits by path, so bump-type detection may
  over-bump when unrelated commits share the history window.
