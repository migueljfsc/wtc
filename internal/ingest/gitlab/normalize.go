package gitlab

import (
	"regexp"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
)

// REST payload structs — fields verified against captured fixtures under
// testdata/gitlab/api/ (fixture-first rule).

// restPipeline is one item of GET /projects/:id/pipelines (and the single-
// pipeline GET). Timestamps are RFC3339; started/finished/duration are null
// while the pipeline is pending.
type restPipeline struct {
	ID         int64      `json:"id"`
	SHA        string     `json:"sha"`
	Ref        string     `json:"ref"`
	Status     string     `json:"status"`
	Source     string     `json:"source"`
	WebURL     string     `json:"web_url"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Duration   *int       `json:"duration"` // seconds
	User       glUser     `json:"user"`
}

// restMergeRequest is one item of GET /projects/:id/merge_requests. SHA is the
// source-branch head; MergeCommitSHA is the commit created on the target — the
// revision GitOps reconciles.
type restMergeRequest struct {
	IID            int        `json:"iid"`
	Title          string     `json:"title"`
	State          string     `json:"state"`
	MergedAt       *time.Time `json:"merged_at"`
	SourceBranch   string     `json:"source_branch"`
	TargetBranch   string     `json:"target_branch"`
	SHA            string     `json:"sha"`
	MergeCommitSHA string     `json:"merge_commit_sha"`
	WebURL         string     `json:"web_url"`
	Author         glUser     `json:"author"`
	MergeUser      *glUser    `json:"merge_user"`
}

// restCommit is one item of GET /projects/:id/repository/commits. The list
// carries no changed-file set (like GitHub's commit list), so push events from
// the poller are path-unknown; the promotion signal (paths + image bump) rides
// the MR-merge event via MR-diff enrichment instead.
type restCommit struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Message       string    `json:"message"`
	AuthorName    string    `json:"author_name"`
	CommittedDate time.Time `json:"committed_date"`
	WebURL        string    `json:"web_url"`
}

// revertTitle spots GitLab's revert convention: "Revert \"…\"" MR/commit
// titles. Same heuristic as the GitHub normalizer.
var revertTitle = regexp.MustCompile(`(?i)^revert\b`)

// NormalizePipeline maps a REST pipeline onto an Event + facts.
func NormalizePipeline(p restPipeline, project string, now time.Time) (*model.Event, normalize.Facts) {
	var finished time.Time
	if p.FinishedAt != nil {
		finished = *p.FinishedAt
	}
	return pipelineEvent(project, p.ID, p.SHA, p.Ref, p.Status, p.WebURL, p.CreatedAt, finished, p.Duration, p.User.actor(), now)
}

// NormalizeMergedMR maps a merged MR onto an Event + facts. Returns nil for
// MRs that are not merged (closed-without-merge is not a change intent).
func NormalizeMergedMR(mr restMergeRequest, project string, now time.Time) (*model.Event, normalize.Facts) {
	if mr.State != "merged" || mr.MergedAt == nil {
		return nil, normalize.Facts{}
	}
	actor := mr.Author.actor()
	if mr.MergeUser != nil && mr.MergeUser.actor() != "" {
		actor = mr.MergeUser.actor()
	}
	return mergedMREvent(project, mr.IID, mr.Title, mr.MergeCommitSHA, mr.TargetBranch, mr.WebURL, actor, *mr.MergedAt, now)
}

// NormalizeCommit maps a default-branch commit onto a push Event + facts.
func NormalizeCommit(c restCommit, project string, now time.Time) (*model.Event, normalize.Facts) {
	title := c.Title
	if title == "" {
		title, _, _ = strings.Cut(c.Message, "\n")
	}
	// List payloads carry no files[]; unknown ≠ no match.
	return pushEvent(project, c.ID, title, c.WebURL, c.AuthorName, nil, true, c.CommittedDate, now)
}
