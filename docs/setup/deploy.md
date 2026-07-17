# Deploying wtc

One binary; the ledger is an embedded SQLite file by default, or an external
Postgres for a stateless pod ([postgres.md](postgres.md), P15). Never scale
horizontally — single writer, per-pod pollers.
Images: `ghcr.io/migueljfsc/wtc` (`latest` + `sha-<sha7>`, published by CI).

## Kubernetes (Helm — the primary packaging)

```bash
kubectl create namespace wtc-system
# ONE operator-managed secret for everything — API tokens, source credentials,
# and (with the postgres backend) DB auth. Opinionated key names; the full
# list is documented in values.yaml.
kubectl -n wtc-system create secret generic wtc-secrets \
  --from-literal=WTC_API_TOKEN=<random> \
  --from-literal=WTC_GH_API_TOKEN=<github PAT> \
  --from-literal=WTC_FLUX_HMAC_KEY=<shared key>
  # postgres backend: add WTC_PG_PASSWORD (bundled) or WTC_STORAGE_DSN (external)

helm install wtc ./deploy/helm/wtc -n wtc-system \
  --set existingSecret=wtc-secrets \
  --values my-values.yaml
```

Put the real `config:` (rules, sources, tag_patterns — SPEC §2) in
`my-values.yaml`. **Only reference `${VAR}`s the secret actually provides**:
an unset variable is a fatal startup error by design.

The chart pins `replicas: 1` + `Recreate` + a ReadWriteOnce PVC — that's the
SQLite contract, not a limitation to tune away. With
`storage.backend=postgres` the pod goes stateless instead: no PVC,
RollingUpdate upgrades, ledger in a bundled or external postgres — see
[postgres.md](postgres.md).

In-cluster URLs: Flux Provider → `http://wtc.wtc-system:8484/ingest/flux`;
CLI from your machine: `kubectl port-forward -n wtc-system svc/wtc 8484:8484`.

## VM / local (docker compose)

```bash
cd deploy/compose
cp wtc.example.yaml wtc.yaml   # edit: rules, sources
WTC_API_TOKEN=<random> docker compose up -d
```

The SQLite file lives in the `wtc-data` volume; `docker compose down` keeps
it, `down -v` deletes it. For a postgres-backed compose stack add the
`docker-compose.postgres.yaml` overlay — see [postgres.md](postgres.md).
