package gitlab

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
)

// Webhook payload structs — fields verified against captured fixtures under
// testdata/gitlab/webhook/ (bodies read back from the project hook-events log,
// GitLab 19.x). The webhook and API shapes differ (webhook nests the resource
// under object_attributes) but the normalizers below feed the same shared
// event-builders as the API path, so both modes converge on identical Events
// and dedup keys.

// glTime tolerates the two timestamp encodings GitLab webhooks emit: RFC3339
// (current, e.g. "2026-07-16T20:07:47.662Z" / "…+00:00") and the older
// space-separated "2006-01-02 15:04:05 UTC" some fields still use. A null or
// empty value decodes to the zero time.
type glTime struct{ time.Time }

func (t *glTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		t.Time = time.Time{}
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700"} {
		if parsed, err := time.Parse(layout, s); err == nil {
			t.Time = parsed
			return nil
		}
	}
	return fmt.Errorf("unrecognized gitlab timestamp %q", s)
}

type webhookProject struct {
	PathWithNamespace string `json:"path_with_namespace"`
}

type pipelineHook struct {
	ObjectAttributes struct {
		ID         int64  `json:"id"`
		Ref        string `json:"ref"`
		SHA        string `json:"sha"`
		Status     string `json:"status"`
		Source     string `json:"source"`
		Duration   *int   `json:"duration"`
		URL        string `json:"url"`
		CreatedAt  glTime `json:"created_at"`
		FinishedAt glTime `json:"finished_at"`
	} `json:"object_attributes"`
	User    glUser         `json:"user"`
	Project webhookProject `json:"project"`
}

type mergeRequestHook struct {
	ObjectAttributes struct {
		IID            int    `json:"iid"`
		Title          string `json:"title"`
		State          string `json:"state"`
		Action         string `json:"action"`
		SourceBranch   string `json:"source_branch"`
		TargetBranch   string `json:"target_branch"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		URL            string `json:"url"`
		UpdatedAt      glTime `json:"updated_at"`
	} `json:"object_attributes"`
	User    glUser         `json:"user"`
	Project webhookProject `json:"project"`
}

type pushHook struct {
	Ref          string         `json:"ref"`
	UserUsername string         `json:"user_username"`
	UserName     string         `json:"user_name"`
	Project      webhookProject `json:"project"`
	Commits      []struct {
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Message   string   `json:"message"`
		URL       string   `json:"url"`
		Timestamp glTime   `json:"timestamp"`
		Added     []string `json:"added"`
		Modified  []string `json:"modified"`
		Removed   []string `json:"removed"`
	} `json:"commits"`
}

// ParseWebhook decodes a delivery body and dispatches on object_kind (the
// X-Gitlab-Event header carries the same signal but the body is authoritative).
// Pipeline hooks yield one Event; a merge action yields one; a push yields one
// per commit (each coalescing with the poller's per-commit push event). A
// non-merge MR action or an unknown kind yields no events — dropped, not
// errored, so the endpoint acknowledges deliveries it deliberately ignores.
func ParseWebhook(raw []byte, now time.Time) ([]EventFacts, error) {
	var env struct {
		ObjectKind string `json:"object_kind"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode gitlab webhook: %w", err)
	}
	switch env.ObjectKind {
	case "pipeline":
		var h pipelineHook
		if err := json.Unmarshal(raw, &h); err != nil {
			return nil, fmt.Errorf("decode pipeline hook: %w", err)
		}
		project := h.Project.PathWithNamespace
		oa := h.ObjectAttributes
		ev, facts := pipelineEvent(project, oa.ID, oa.SHA, oa.Ref, oa.Status, oa.URL, oa.CreatedAt.Time, oa.FinishedAt.Time, oa.Duration, h.User.actor(), now)
		return []EventFacts{{ev, facts}}, nil
	case "merge_request":
		var h mergeRequestHook
		if err := json.Unmarshal(raw, &h); err != nil {
			return nil, fmt.Errorf("decode merge request hook: %w", err)
		}
		oa := h.ObjectAttributes
		if oa.Action != "merge" {
			return nil, nil // only the merge transition is a change intent
		}
		ev, facts := mergedMREvent(h.Project.PathWithNamespace, oa.IID, oa.Title, oa.MergeCommitSHA, oa.TargetBranch, oa.URL, h.User.actor(), oa.UpdatedAt.Time, now)
		return []EventFacts{{ev, facts}}, nil
	case "push":
		var h pushHook
		if err := json.Unmarshal(raw, &h); err != nil {
			return nil, fmt.Errorf("decode push hook: %w", err)
		}
		actor := h.UserUsername
		if actor == "" {
			actor = h.UserName
		}
		var out []EventFacts
		for _, c := range h.Commits {
			paths := append(append(append([]string{}, c.Added...), c.Modified...), c.Removed...)
			title := c.Title
			if title == "" {
				title, _, _ = strings.Cut(c.Message, "\n")
			}
			// The commit's file set is present in the push payload, so paths
			// are known (not truncated) even when empty — a real "touched
			// nothing under an env path" push resolves to env="" honestly.
			ev, facts := pushEvent(h.Project.PathWithNamespace, c.ID, title, c.URL, actor, paths, false, c.Timestamp.Time, now)
			out = append(out, EventFacts{ev, facts})
		}
		return out, nil
	}
	return nil, nil
}

// EventFacts pairs a normalized event with its inference facts, so the webhook
// handler and poller run them through the same engine.Apply + store.Ingest.
type EventFacts struct {
	Event *model.Event
	Facts normalize.Facts
}
