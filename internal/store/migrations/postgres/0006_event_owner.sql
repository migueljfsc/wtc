-- 0006: owning team as a first-class, facetable column. Inferred at ingest
-- from `service` via the catalog scan (backstage/datadog/services.yaml/
-- CODEOWNERS); stays '' when no catalog match. Denormalized on purpose so the
-- value is point-in-time (the owner as of the change) and reuses the existing
-- facet/filter/notify machinery. Append-only.
ALTER TABLE events ADD COLUMN owner TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_events_owner_ts ON events(owner, ts);
