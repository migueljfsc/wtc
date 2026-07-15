# wtc — what the change

A vendor-neutral change ledger: **"git log for production"**. One self-hosted
binary that ingests change events from heterogeneous sources — CI builds,
GitOps reconciles, helm/terraform runs, manual changes — normalizes them into
one schema, and answers three questions fast:

```bash
wtc log --env prod --since 2h        # what changed?
wtc where 4f2a91c                    # where is this commit? build → merge → per-env deploy, with lag
wtc diff staging prod                # how do two environments differ right now?
```

New Relic, Datadog, and Harness sell change tracking locked inside their
platforms. wtc is neutral, standalone, CLI-first, and runs happily inside a
private network — the GitHub poller pulls instead of waiting for webhooks.

## Status

**Phases 0–6 complete** (see [docs/PLAN.md](docs/PLAN.md)). Working today,
each verified against live infrastructure:

- **Ingest**: GitHub (API poller primary, HMAC webhooks for public endpoints),
  Flux notification-controller (generic-hmac), Alertmanager, `/ingest/generic`,
  `wtc record`, `wtc wrap` (helm/terraform sniffers)
- **Queries**: `log` (FTS5 `-q`), `where` (build → intent → applied per env,
  tag↔sha via configurable `tag_patterns`), `diff`, `handoff`, `around`, `doctor`
- **Engine**: ordered env/service inference rules with globs + templates;
  strict-outrank dedup upsert (at-least-once ingestion, zero duplicates);
  PR-diff enrichment; redaction before storage
- **Surfaces**: embedded timeline UI at `/`, Slack digest, `wtc demo` seed
- **Ops**: opt-in retention prune (`pr-*` ephemeral windows + `incremental_vacuum`)
- **Packaging**: `ghcr.io/migueljfsc/wtc` multi-arch image (auto-versioned),
  goreleaser binaries (linux/darwin × amd64/arm64), Helm chart, docker-compose

Remaining: a **rich portal UI** track (P7–P10: dashboards, metrics,
change-intelligence views) built as a separate SPA alongside the embedded
timeline.

## Quickstart (local)

Fastest way to see the whole thing — API + portal UI + a seeded week of fake
data, one command:

```bash
docker compose up --build        # or: make demo
# → open http://localhost:8080 and sign in with the token:  demo-token
```

Everything is self-contained (ephemeral DB, no secrets, no real GitHub/Flux).
For a real deployment see [deploy/docker-compose.yaml](deploy/docker-compose.yaml)
+ [docs/setup/portal.md](docs/setup/portal.md).

### From source (CLI only)

```bash
make build
./bin/wtc init                        # scaffolds wtc.yaml + prints checklist
export WTC_API_TOKEN=$(openssl rand -hex 16)
export WTC_GH_API_TOKEN=<github PAT>  # read-only: Actions/Contents/PRs
./bin/wtc serve --config wtc.yaml &

# Fastest way to see it work — seed a synthetic week (no real sources needed):
./bin/wtc demo
./bin/wtc log --since 7d
./bin/wtc diff staging prod

# Or record a change by hand:
./bin/wtc record --kind manual --env prod --title "rotated db creds"
./bin/wtc doctor
```

Wiring real sources: [docs/setup/github-poller.md](docs/setup/github-poller.md) ·
[docs/setup/flux.md](docs/setup/flux.md) · [docs/setup/wrap.md](docs/setup/wrap.md) ·
deploy via [docs/setup/deploy.md](docs/setup/deploy.md) ·
[retention](docs/setup/retention.md).

## How it works

```
GitHub API poller ─┐
GitHub webhooks ───┤   parsers    ┌─ rules engine ─┐    SQLite (WAL)
Flux notifications ┼─ (fixture- ─→│ env/service    │──→ one events table ──→ log/where/diff/
/ingest/generic ───┤   tested)    │ inference      │    dedup_key upsert     handoff/doctor
wtc record/wrap ───┘              └─ + redaction ──┘                          (CLI + JSON API)
```

Design pillars (full rationale in [CLAUDE.md](CLAUDE.md), schema/API contract
in [docs/SPEC.md](docs/SPEC.md)):

- **One row per logical change** — status transitions upsert in place, keyed
  by a `dedup_key` derived from source-side identifiers. Lost webhooks,
  poller re-reads, and Flux re-emits are all harmless replays.
- **Never guess** — events whose env can't be inferred land with `env=""`
  and are surfaced by `wtc doctor`, not misrouted.
- **Fixture-first** — every normalizer is built against real captured
  payloads frozen under `testdata/`, never against documentation memory.
- **The CLI never opens the DB** — everything goes through the serve API.

## Repository map

| Path | What it is |
|---|---|
| `cmd/wtc/` | cobra CLI: serve, record, log, where, diff, handoff, wrap, doctor, init |
| `internal/` | the engine — see [internal/README.md](internal/README.md) |
| `deploy/` | Helm chart + docker-compose — see [deploy/README.md](deploy/README.md) |
| `demo/` | three dummy services generating real events end-to-end — see [demo/README.md](demo/README.md) |
| `testdata/` | frozen real payloads (the normalizer contract) — see [testdata/README.md](testdata/README.md) |
| `docs/` | SPEC (schema/API), PLAN (phases), setup/ (wiring guides) |
| `.github/workflows/` | wtc CI/publish + demo pipelines — see [.github/workflows/README.md](.github/workflows/README.md) |

## License

Apache-2.0
