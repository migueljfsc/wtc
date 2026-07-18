-- 0005: source repo (owner/name) as a first-class, facetable column. Populated
-- on source/CI events (github/gitlab PR/push/build); stays '' for cluster-side
-- events (flux/argo) whose payloads carry no source repo. Append-only.
ALTER TABLE events ADD COLUMN repo TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_events_repo_ts ON events(repo, ts);
