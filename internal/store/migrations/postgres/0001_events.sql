-- 0001 (postgres): events table — the same logical schema as the sqlite 0001.
-- Timestamps stay RFC3339 UTC TEXT (fixed ms precision, lexicographically
-- sortable) so every query and the Go scan layer are identical across
-- backends. duration_ms is BIGINT: sqlite INTEGER is 64-bit, postgres's isn't.
-- Append-only migration — never edit after it has been applied.
CREATE TABLE events (
  id          TEXT PRIMARY KEY,            -- ULID
  ts          TEXT NOT NULL,               -- RFC3339 UTC (fixed ms precision), source event time
  ingested_at TEXT NOT NULL,               -- RFC3339 UTC (fixed ms precision)
  source      TEXT NOT NULL,
  kind        TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'unknown',
  env         TEXT NOT NULL DEFAULT '',    -- '' = unmapped, surfaced by doctor
  cluster     TEXT NOT NULL DEFAULT '',
  namespace   TEXT NOT NULL DEFAULT '',
  service     TEXT NOT NULL DEFAULT '',
  actor       TEXT NOT NULL DEFAULT '',
  ref         TEXT NOT NULL DEFAULT '',
  artifact    TEXT NOT NULL DEFAULT '',
  title       TEXT NOT NULL,
  url         TEXT NOT NULL DEFAULT '',
  duration_ms BIGINT,
  dedup_key   TEXT NOT NULL UNIQUE,
  payload     TEXT
);

CREATE INDEX idx_events_ts         ON events(ts);
CREATE INDEX idx_events_env_ts     ON events(env, ts);
CREATE INDEX idx_events_service_ts ON events(service, ts);
CREATE INDEX idx_events_ref        ON events(ref);
CREATE INDEX idx_events_kind_ts    ON events(kind, ts);
