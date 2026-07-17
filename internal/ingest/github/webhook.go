package github

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
)

// Webhook envelope parsing (P13). GitHub delivers the same resource objects the
// REST API returns, wrapped in an event envelope and dispatched by the
// X-GitHub-Event header. The nested `workflow_run` and `pull_request` objects
// are field-identical to the poller's restWorkflowRun/restPullRequest, so the
// envelopes reuse those structs and call the SAME normalizers — webhook and
// poller therefore converge on identical Events and dedup keys, and the poller
// stays the idempotent loss-recovery sweeper. Shapes verified against captured
// fixtures under testdata/github/webhook/ (real deliveries read back from the
// hook-deliveries API).

// EventFacts pairs a normalized event with its inference facts so the webhook
// handler runs them through the same engine.Apply + store.Ingest as the poller.
type EventFacts struct {
	Event *model.Event
	Facts normalize.Facts
}

type workflowRunHook struct {
	Action      string          `json:"action"`
	WorkflowRun restWorkflowRun `json:"workflow_run"`
}

type pullRequestHook struct {
	Action      string          `json:"action"`
	PullRequest restPullRequest `json:"pull_request"`
	Repository  restRepo        `json:"repository"`
}

type pushHook struct {
	Ref        string   `json:"ref"`
	Repository restRepo `json:"repository"`
	Sender     restUser `json:"sender"`
	Commits    []struct {
		ID      string `json:"id"`
		Message string `json:"message"`
		URL     string `json:"url"`
		Author  struct {
			Name     string `json:"name"`
			Username string `json:"username"`
		} `json:"author"`
		Timestamp time.Time `json:"timestamp"`
		Added     []string  `json:"added"`
		Modified  []string  `json:"modified"`
		Removed   []string  `json:"removed"`
	} `json:"commits"`
}

// ParseWebhook decodes a delivery body, dispatching on the X-GitHub-Event
// header (event). A workflow_run yields one build Event; a merged pull_request
// yields one merge Event; a push yields one per commit (each coalescing with
// the poller's per-commit push). A non-merge PR action, an unmerged close, or
// an unhandled event yields no events — acknowledged, not errored, so the
// endpoint returns 2xx for deliveries it deliberately ignores.
func ParseWebhook(event string, raw []byte, now time.Time) ([]EventFacts, error) {
	switch event {
	case "workflow_run":
		var h workflowRunHook
		if err := json.Unmarshal(raw, &h); err != nil {
			return nil, fmt.Errorf("decode workflow_run hook: %w", err)
		}
		ev, facts := NormalizeWorkflowRun(h.WorkflowRun, now)
		return []EventFacts{{ev, facts}}, nil

	case "pull_request":
		var h pullRequestHook
		if err := json.Unmarshal(raw, &h); err != nil {
			return nil, fmt.Errorf("decode pull_request hook: %w", err)
		}
		// Only a merge is a change intent. NormalizeMergedPR returns nil for an
		// unmerged PR (closed-without-merge), so a non-"closed" action or a
		// closed-unmerged PR both drop out here.
		if h.Action != "closed" {
			return nil, nil
		}
		repo := h.Repository.FullName
		ev, facts := NormalizeMergedPR(h.PullRequest, repo, now)
		if ev == nil {
			return nil, nil
		}
		return []EventFacts{{ev, facts}}, nil

	case "push":
		var h pushHook
		if err := json.Unmarshal(raw, &h); err != nil {
			return nil, fmt.Errorf("decode push hook: %w", err)
		}
		repo := h.Repository.FullName
		var out []EventFacts
		for _, c := range h.Commits {
			actor := c.Author.Username
			if actor == "" {
				actor = c.Author.Name
			}
			if actor == "" {
				actor = h.Sender.Login
			}
			title, _, _ := strings.Cut(c.Message, "\n")
			paths := append(append(append([]string{}, c.Added...), c.Modified...), c.Removed...)
			// The commit file set is present in the push payload, so paths are
			// known (not truncated). GitHub caps commits[] at ~20 per push and
			// omits file arrays on very large commits; the poller sweeps any
			// commit the webhook missed (idempotent by dedup key), so a capped
			// push under-reports at worst, never misroutes (trap #3).
			ev, facts := pushEvent(repo, c.ID, title, c.URL, actor, paths, false, c.Timestamp, now)
			out = append(out, EventFacts{ev, facts})
		}
		return out, nil
	}
	return nil, nil
}
