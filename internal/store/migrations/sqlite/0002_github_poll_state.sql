-- 0002: per-repo high-water marks for the GitHub API poller (PLAN P1).
-- Append-only migration — never edit after it has been applied.
CREATE TABLE github_poll_state (
  repo       TEXT NOT NULL,               -- owner/name
  resource   TEXT NOT NULL,               -- runs | prs | commits
  watermark  TEXT NOT NULL,               -- RFC3339 UTC; newest source timestamp seen
  updated_at TEXT NOT NULL,
  PRIMARY KEY (repo, resource)
);
