package github

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
)

// REST payload structs — fields verified against captured fixtures under
// testdata/github/rest/ (fixture-first rule). Webhook envelope parsing is
// deferred until webhook fixtures exist.

type restWorkflowRunList struct {
	WorkflowRuns []restWorkflowRun `json:"workflow_runs"`
}

type restWorkflowRun struct {
	ID              int64     `json:"id"`
	RunAttempt      int       `json:"run_attempt"`
	RunNumber       int       `json:"run_number"`
	Name            string    `json:"name"`
	Status          string    `json:"status"`
	Conclusion      string    `json:"conclusion"`
	HeadSHA         string    `json:"head_sha"`
	HeadBranch      string    `json:"head_branch"`
	Event           string    `json:"event"`
	HTMLURL         string    `json:"html_url"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	RunStartedAt    time.Time `json:"run_started_at"`
	Actor           restUser  `json:"actor"`
	TriggeringActor restUser  `json:"triggering_actor"`
	Repository      restRepo  `json:"repository"`
}

type restUser struct {
	Login string `json:"login"`
}

type restRepo struct {
	FullName string `json:"full_name"`
}

type restPullRequest struct {
	Number         int        `json:"number"`
	Title          string     `json:"title"`
	MergedAt       *time.Time `json:"merged_at"`
	MergeCommitSHA string     `json:"merge_commit_sha"`
	HTMLURL        string     `json:"html_url"`
	User           restUser   `json:"user"`
	Base           struct {
		Ref  string   `json:"ref"`
		Repo restRepo `json:"repo"`
	} `json:"base"`
}

type restCommit struct {
	SHA     string    `json:"sha"`
	HTMLURL string    `json:"html_url"`
	Author  *restUser `json:"author"` // null for non-GitHub authors
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Name string    `json:"name"`
			Date time.Time `json:"date"`
		} `json:"author"`
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

// runStatus maps GitHub's status/conclusion pair onto the event lifecycle.
// queued/in_progress → started; completed splits on conclusion; anything
// unrecognized is unknown, never guessed.
func runStatus(status, conclusion string) model.Status {
	switch status {
	case "queued", "in_progress", "requested", "waiting", "pending":
		return model.StatusStarted
	case "completed":
		switch conclusion {
		case "success":
			return model.StatusSucceeded
		case "failure", "cancelled", "timed_out", "startup_failure", "action_required":
			return model.StatusFailed
		}
	}
	return model.StatusUnknown
}

// NormalizeWorkflowRun maps one REST workflow run onto an Event + facts.
// dedup gh:run:<repo>:<id>:<attempt> — one row per attempt, status upserted
// across queued→in_progress→completed.
func NormalizeWorkflowRun(run restWorkflowRun, now time.Time) (*model.Event, normalize.Facts) {
	status := runStatus(run.Status, run.Conclusion)
	repo := run.Repository.FullName

	ts := run.RunStartedAt
	if ts.IsZero() {
		ts = run.CreatedAt
	}
	var durationMS *int64
	if run.Status == "completed" {
		ts = run.UpdatedAt
		if !run.RunStartedAt.IsZero() && run.UpdatedAt.After(run.RunStartedAt) {
			d := run.UpdatedAt.Sub(run.RunStartedAt).Milliseconds()
			durationMS = &d
		}
	}

	statusWord := run.Status
	if run.Status == "completed" && run.Conclusion != "" {
		statusWord = run.Conclusion
	}
	actor := run.TriggeringActor.Login
	if actor == "" {
		actor = run.Actor.Login
	}

	ev := &model.Event{
		ID:         model.NewID(),
		TS:         ts.UTC(),
		IngestedAt: now.UTC(),
		Source:     model.SourceGitHub,
		Kind:       model.KindBuild,
		Status:     status,
		Actor:      actor,
		Ref:        run.HeadSHA,
		Title:      fmt.Sprintf("%s #%d %s (%s)", run.Name, run.RunNumber, statusWord, run.HeadBranch),
		URL:        run.HTMLURL,
		DurationMS: durationMS,
		DedupKey:   fmt.Sprintf("gh:run:%s:%d:%d", repo, run.ID, run.RunAttempt),
	}
	facts := normalize.Facts{
		Source:   "github",
		Repo:     repo,
		Branch:   run.HeadBranch,
		Event:    "workflow_run",
		Workflow: run.Name, // service signal for monorepos (per-service workflows)
		Actor:    actor,
		// Changed files are not part of run payloads.
		PathsTruncated: true,
	}
	return ev, facts
}

// revertTitle spots GitHub's revert-PR convention: the UI generates titles
// like `Revert "original title"` and branches like revert-123-branch.
var revertTitle = regexp.MustCompile(`(?i)^revert\b`)

// NormalizeMergedPR maps a merged pull request onto an Event + facts. Returns
// nil for unmerged PRs (closed-without-merge is not a change intent).
// repo comes from the poller scope; list payloads carry it in base.repo too.
// Revert PRs land as kind=rollback via revert-title detection.
func NormalizeMergedPR(pr restPullRequest, repo string, now time.Time) (*model.Event, normalize.Facts) {
	if pr.MergedAt == nil {
		return nil, normalize.Facts{}
	}
	if pr.Base.Repo.FullName != "" {
		repo = pr.Base.Repo.FullName
	}

	kind := model.KindMerge
	if revertTitle.MatchString(pr.Title) {
		kind = model.KindRollback
	}

	ev := &model.Event{
		ID:         model.NewID(),
		TS:         pr.MergedAt.UTC(),
		IngestedAt: now.UTC(),
		Source:     model.SourceGitHub,
		Kind:       kind,
		Status:     model.StatusSucceeded,
		Actor:      pr.User.Login,
		Ref:        pr.MergeCommitSHA,
		Title:      fmt.Sprintf("PR #%d merged: %s", pr.Number, pr.Title),
		URL:        pr.HTMLURL,
		DedupKey:   fmt.Sprintf("gh:pr:%s:%d:merged", repo, pr.Number),
	}
	facts := normalize.Facts{
		Source: "github",
		Repo:   repo,
		Branch: pr.Base.Ref,
		Event:  "pull_request",
		Actor:  pr.User.Login,
		// Changed files need the PR-files API — unknown here.
		PathsTruncated: true,
	}
	return ev, facts
}

// NormalizeCommit maps a default-branch commit onto a push Event + facts.
// dedup gh:push:<repo>:<sha>, so webhook pushes and polled commits coalesce.
func NormalizeCommit(c restCommit, repo string, now time.Time) (*model.Event, normalize.Facts) {
	actor := c.Commit.Author.Name
	if c.Author != nil && c.Author.Login != "" {
		actor = c.Author.Login
	}
	title, _, _ := strings.Cut(c.Commit.Message, "\n")

	ts := c.Commit.Committer.Date
	if ts.IsZero() {
		ts = c.Commit.Author.Date
	}
	// Commit-list payloads carry no files[]; per-commit detail fetch is a
	// separate enrichment step. Unknown ≠ no match.
	return pushEvent(repo, c.SHA, title, c.HTMLURL, actor, nil, true, ts, now)
}

// pushEvent builds the Event + facts for one commit, shared by the poller
// (NormalizeCommit) and the webhook push parser so both converge on the same
// gh:push:<repo>:<sha> row. paths (known from a webhook push's
// added/modified/removed) drive path-based env inference; the poller's
// commit-list carries none, so it passes truncated=true.
func pushEvent(repo, sha, title, url, actor string, paths []string, truncated bool, ts, now time.Time) (*model.Event, normalize.Facts) {
	if ts.IsZero() {
		ts = now
	}
	ev := &model.Event{
		ID:         model.NewID(),
		TS:         ts.UTC(),
		IngestedAt: now.UTC(),
		Source:     model.SourceGitHub,
		Kind:       model.KindPush,
		Status:     model.StatusSucceeded,
		Actor:      actor,
		Ref:        sha,
		Title:      title,
		URL:        url,
		DedupKey:   fmt.Sprintf("gh:push:%s:%s", repo, sha),
	}
	facts := normalize.Facts{
		Source:         "github",
		Repo:           repo,
		Event:          "push",
		Actor:          actor,
		Paths:          paths,
		PathsTruncated: truncated,
	}
	return ev, facts
}
