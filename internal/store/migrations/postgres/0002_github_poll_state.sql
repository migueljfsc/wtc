-- 0002 (postgres): per-repo high-water marks for the API pollers — identical
-- to the sqlite 0002. Append-only migration — never edit after it has been
-- applied.
CREATE TABLE github_poll_state (
  repo       TEXT NOT NULL,               -- owner/name
  resource   TEXT NOT NULL,               -- runs | prs | commits
  watermark  TEXT NOT NULL,               -- RFC3339 UTC; newest source timestamp seen
  updated_at TEXT NOT NULL,
  PRIMARY KEY (repo, resource)
);
