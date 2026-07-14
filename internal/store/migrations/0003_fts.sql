-- 0003: FTS5 external-content index over the searchable event fields, kept in
-- sync by triggers. Backs `wtc log -q`. Append-only migration.
CREATE VIRTUAL TABLE events_fts USING fts5(
  title, service, actor, artifact,
  content='events',
  content_rowid='rowid'
);

CREATE TRIGGER events_fts_ai AFTER INSERT ON events BEGIN
  INSERT INTO events_fts(rowid, title, service, actor, artifact)
  VALUES (new.rowid, new.title, new.service, new.actor, new.artifact);
END;

CREATE TRIGGER events_fts_ad AFTER DELETE ON events BEGIN
  INSERT INTO events_fts(events_fts, rowid, title, service, actor, artifact)
  VALUES ('delete', old.rowid, old.title, old.service, old.actor, old.artifact);
END;

CREATE TRIGGER events_fts_au AFTER UPDATE ON events BEGIN
  INSERT INTO events_fts(events_fts, rowid, title, service, actor, artifact)
  VALUES ('delete', old.rowid, old.title, old.service, old.actor, old.artifact);
  INSERT INTO events_fts(rowid, title, service, actor, artifact)
  VALUES (new.rowid, new.title, new.service, new.actor, new.artifact);
END;

-- Backfill existing rows.
INSERT INTO events_fts(rowid, title, service, actor, artifact)
SELECT rowid, title, service, actor, artifact FROM events;
