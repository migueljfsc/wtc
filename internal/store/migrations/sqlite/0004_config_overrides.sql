-- 0004: DB-backed overrides for the editable normalization config (rules,
-- tag_patterns), so the portal can hot-reload them without a restart or a
-- writable config file (both shipped deployments mount wtc.yaml read-only).
--
-- Precedence: a key is absent until the operator saves an override in the UI;
-- absent => the engine uses the YAML value. Deleting a key resets to YAML.
CREATE TABLE config_overrides (
  key        TEXT PRIMARY KEY,   -- "rules" | "tag_patterns"
  value      TEXT NOT NULL,      -- JSON
  updated_at TEXT NOT NULL       -- UTC RFC3339
);
