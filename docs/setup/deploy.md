# Deploying wtc

One binary, one SQLite file. Never scale horizontally — single writer.
Images: `ghcr.io/migueljfsc/wtc` (`latest` + `sha-<sha7>`, published by CI).

## Kubernetes (Helm — the primary packaging)

```bash
kubectl create namespace wtc-system
kubectl -n wtc-system create secret generic wtc-tokens \
  --from-literal=WTC_API_TOKEN=<random> \
  --from-literal=WTC_GH_API_TOKEN=<github PAT> \
  --from-literal=WTC_FLUX_HMAC_KEY=<shared key>

helm install wtc ./deploy/helm/wtc -n wtc-system \
  --set existingSecret=wtc-tokens \
  --values my-values.yaml
```

Put the real `config:` (rules, sources, tag_patterns — SPEC §2) in
`my-values.yaml`. **Only reference `${VAR}`s the secret actually provides**:
an unset variable is a fatal startup error by design.

The chart pins `replicas: 1` + `Recreate` + a ReadWriteOnce PVC — that's the
SQLite contract, not a limitation to tune away.

In-cluster URLs: Flux Provider → `http://wtc.wtc-system:8484/ingest/flux`;
CLI from your machine: `kubectl port-forward -n wtc-system svc/wtc 8484:8484`.

## VM / local (docker compose)

```bash
cd deploy
cp wtc.example.yaml wtc.yaml   # edit: rules, sources
WTC_API_TOKEN=<random> docker compose up -d
```

The SQLite file lives in the `wtc-data` volume; `docker compose down` keeps
it, `down -v` deletes it.
