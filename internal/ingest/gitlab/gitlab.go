// Package gitlab implements the GitLab ingest paths: the API poller (primary
// for private deployments, mirroring the GitHub poller) and the webhook
// receiver together with internal/server. It is the SCM/CI-axis
// vendor-neutrality proof (GitHub↔GitLab, as Flux↔Argo was for GitOps).
//
// Both modes converge on the same normalized Events and dedup keys — the
// poller doubles as the webhook-loss sweeper (idempotent by design), exactly
// like GitHub. Parsers are written against captured fixtures under
// testdata/gitlab/ (gitlab.com free project, GitLab 19.x), never documentation
// memory.
//
// The project's `path_with_namespace` (e.g. "group/service") plays the role
// GitHub's "owner/repo" does: it is the human-readable, stable identifier in
// facts and dedup keys. GitLab's numeric project_id is used only to address the
// REST API.
package gitlab

import (
	"fmt"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
)

// glUser is the actor shape shared by pipeline/MR/commit payloads (both API
// and webhook). Username is the stable handle; Name is the display name.
type glUser struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

func (u glUser) actor() string {
	if u.Username != "" {
		return u.Username
	}
	return u.Name
}

// pipelineStatus maps GitLab's pipeline status onto the event lifecycle.
// created/waiting/preparing/pending/running/scheduled → started; success →
// succeeded; failed/canceled → failed (canceled mirrors GitHub's cancelled);
// anything else (skipped, manual) is unknown, never guessed. Same shape as
// GitHub's runStatus (normalize.go there).
func pipelineStatus(status string) model.Status {
	switch status {
	case "created", "waiting_for_resource", "preparing", "pending", "running", "scheduled":
		return model.StatusStarted
	case "success":
		return model.StatusSucceeded
	case "failed", "canceled", "cancelled":
		return model.StatusFailed
	}
	return model.StatusUnknown
}

// pipelineEvent builds the normalized Event + facts for a CI pipeline from
// primitives extracted by either the API or webhook parser, so the two modes
// produce byte-identical Events and dedup keys (convergence is what lets the
// poller sweep webhook losses). dedup gl:pipeline:<project>:<id> — the GitLab
// pipeline id is stable across queued→running→completed, so one row is
// upserted across the lifecycle; a *retried* pipeline gets a fresh
// id and is a truthful second row.
func pipelineEvent(project string, id int64, sha, ref, status, url string, created, finished time.Time, durationSec *int, actor string, now time.Time) (*model.Event, normalize.Facts) {
	st := pipelineStatus(status)

	ts := created
	var durationMS *int64
	if isTerminal(st) && !finished.IsZero() {
		ts = finished
		if durationSec != nil && *durationSec > 0 {
			d := int64(*durationSec) * 1000
			durationMS = &d
		} else if !created.IsZero() && finished.After(created) {
			d := finished.Sub(created).Milliseconds()
			durationMS = &d
		}
	}
	if ts.IsZero() {
		ts = now
	}

	ev := &model.Event{
		ID:         model.NewID(),
		TS:         ts.UTC(),
		IngestedAt: now.UTC(),
		Source:     model.SourceGitLab,
		Kind:       model.KindBuild,
		Status:     st,
		Actor:      actor,
		Ref:        sha,
		Title:      fmt.Sprintf("Pipeline #%d %s (%s)", id, status, ref),
		URL:        url,
		DurationMS: durationMS,
		DedupKey:   fmt.Sprintf("gl:pipeline:%s:%d", project, id),
	}
	facts := normalize.Facts{
		Source: "gitlab",
		Repo:   project,
		Branch: ref,
		Event:  "pipeline",
		Actor:  actor,
		// Pipeline payloads carry no changed-file list.
		PathsTruncated: true,
	}
	return ev, facts
}

// mergedMREvent builds the Event + facts for a merged MR. dedup
// gl:mr:<project>:<iid>:merged. Revert MRs land as kind=rollback, mirroring
// GitHub's merged-PR heuristic. Ref is the merge commit sha (the revision Flux
// or Argo reconciles), so the where join spans merge→apply. Enrichment (real
// paths + image bumps from the MR diff) is layered on by the caller.
func mergedMREvent(project string, iid int, title, mergeCommitSHA, targetBranch, url, actor string, mergedAt, now time.Time) (*model.Event, normalize.Facts) {
	kind := model.KindMerge
	if revertTitle.MatchString(title) {
		kind = model.KindRollback
	}
	ts := mergedAt
	if ts.IsZero() {
		ts = now
	}
	ev := &model.Event{
		ID:         model.NewID(),
		TS:         ts.UTC(),
		IngestedAt: now.UTC(),
		Source:     model.SourceGitLab,
		Kind:       kind,
		Status:     model.StatusSucceeded,
		Actor:      actor,
		Ref:        mergeCommitSHA,
		Title:      fmt.Sprintf("MR !%d merged: %s", iid, title),
		URL:        url,
		DedupKey:   fmt.Sprintf("gl:mr:%s:%d:merged", project, iid),
	}
	facts := normalize.Facts{
		Source: "gitlab",
		Repo:   project,
		Branch: targetBranch,
		Event:  "merge_request",
		Actor:  actor,
		// Changed files come from MR-diff enrichment; unknown until then.
		PathsTruncated: true,
	}
	return ev, facts
}

// pushEvent builds the Event + facts for one default-branch commit. dedup
// gl:push:<project>:<sha>, so webhook pushes and polled commits coalesce.
// paths (when known, from the webhook commit's added/modified/removed) drive
// path-based env inference; the poller's commit-list carries none, so it
// passes truncated=true (unknown ≠ no match).
func pushEvent(project, sha, title, url, actor string, paths []string, truncated bool, ts, now time.Time) (*model.Event, normalize.Facts) {
	if ts.IsZero() {
		ts = now
	}
	ev := &model.Event{
		ID:         model.NewID(),
		TS:         ts.UTC(),
		IngestedAt: now.UTC(),
		Source:     model.SourceGitLab,
		Kind:       model.KindPush,
		Status:     model.StatusSucceeded,
		Actor:      actor,
		Ref:        sha,
		Title:      title,
		URL:        url,
		DedupKey:   fmt.Sprintf("gl:push:%s:%s", project, sha),
	}
	facts := normalize.Facts{
		Source:         "gitlab",
		Repo:           project,
		Event:          "push",
		Actor:          actor,
		Paths:          paths,
		PathsTruncated: truncated,
	}
	return ev, facts
}

func isTerminal(s model.Status) bool {
	return s == model.StatusSucceeded || s == model.StatusFailed
}

// Strings that make suppressed-vs-stored observable in handler responses
// (mirrors flux/argocd; kept per-package so ingest packages stay independent).
const (
	ResultStored     = "stored"
	ResultSuppressed = "suppressed"
)
