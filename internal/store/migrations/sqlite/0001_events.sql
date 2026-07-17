-- 0001: events table. Append-only migration — never edit after it has been applied.
CREATE TABLE events (
  id          TEXT PRIMARY KEY,            -- ULID
  ts          TEXT NOT NULL,               -- RFC3339 UTC (fixed ms precision), source event time
  ingested_at TEXT NOT NULL,               -- RFC3339 UTC (fixed ms precision)
  source      TEXT NOT NULL,               -- github | flux | helm | terraform | manual | generic | alertmanager
  kind        TEXT NOT NULL,               -- build | merge | push | deploy | config_change | infra_change | rollback | alert | manual
  status      TEXT NOT NULL DEFAULT 'unknown', -- started | succeeded | failed | unknown
  env         TEXT NOT NULL DEFAULT '',    -- prod | staging | dev | pr-123 | '' (unmapped)
  cluster     TEXT NOT NULL DEFAULT '',
  namespace   TEXT NOT NULL DEFAULT '',
  service     TEXT NOT NULL DEFAULT '',
  actor       TEXT NOT NULL DEFAULT '',
  ref         TEXT NOT NULL DEFAULT '',
  artifact    TEXT NOT NULL DEFAULT '',
  title       TEXT NOT NULL,
  url         TEXT NOT NULL DEFAULT '',
  duration_ms INTEGER,
  dedup_key   TEXT NOT NULL UNIQUE,
  payload     TEXT
);

CREATE INDEX idx_events_ts         ON events(ts);
CREATE INDEX idx_events_env_ts     ON events(env, ts);
CREATE INDEX idx_events_service_ts ON events(service, ts);
CREATE INDEX idx_events_ref        ON events(ref);
CREATE INDEX idx_events_kind_ts    ON events(kind, ts);
