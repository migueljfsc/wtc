-- 0003 (postgres): DB-backed overrides for the editable normalization config —
-- identical to the sqlite 0004. There is deliberately NO FTS migration on
-- postgres (sqlite 0003): `wtc log -q` uses per-term ILIKE there; the events
-- table is small enough that pg_trgm is not warranted yet. Append-only
-- migration — never edit after it has been applied.
CREATE TABLE config_overrides (
  key        TEXT PRIMARY KEY,   -- "rules" | "tag_patterns"
  value      TEXT NOT NULL,      -- JSON
  updated_at TEXT NOT NULL       -- UTC RFC3339
);
