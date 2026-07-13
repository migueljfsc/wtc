package github

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

const fixtureDir = "../../../testdata/github/rest"

func loadFixture(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
}

var testNow = time.Date(2026, 7, 13, 16, 0, 0, 0, time.UTC)

// Golden tests: real captured payloads → expected normalized Events.

func TestGoldenWorkflowRuns(t *testing.T) {
	tests := []struct {
		fixture      string
		wantStatus   model.Status
		wantTS       time.Time
		wantDuration *int64
		wantActor    string
		wantRef      string
		wantTitle    string
		wantDedup    string
	}{
		{
			fixture:      "workflow_run_completed_success.json",
			wantStatus:   model.StatusSucceeded,
			wantTS:       time.Date(2026, 7, 13, 15, 12, 15, 0, time.UTC), // updated_at
			wantDuration: ptr(int64(26_000)),                              // run_started→updated
			wantActor:    "migueljfsc",
			wantRef:      "f1945371e63a7556860fad1555be40a4a0d736a8",
			wantTitle:    "ci #3 success (main)",
			wantDedup:    "gh:run:migueljfsc/wtc:29261201471:1",
		},
		{
			fixture:      "workflow_run_completed_failure.json",
			wantStatus:   model.StatusFailed,
			wantTS:       time.Date(2026, 7, 12, 22, 36, 52, 0, time.UTC),
			wantDuration: ptr(int64(22_000)),
			wantActor:    "dependabot[bot]",
			wantRef:      "f0da9accbb7ad4206a9a231863a36c659bade658",
			wantTitle:    "github_actions in /. - Update #1457172736 #4 failure (main)",
			wantDedup:    "gh:run:migueljfsc/wtc:29211857530:1",
		},
		{
			fixture:    "workflow_run_queued.json",
			wantStatus: model.StatusStarted,
			// queued: run_started_at absent in some payloads — ours has it
			// (rerun), so ts = run_started_at.
			wantTS:    time.Date(2026, 7, 13, 15, 34, 38, 0, time.UTC),
			wantActor: "migueljfsc",
			wantRef:   "a43e0f0e815fbccb3fe0be2fba5085135ab7f7df",
			wantTitle: "ci #4 queued (main)",
			wantDedup: "gh:run:migueljfsc/wtc:29262668304:2",
		},
		{
			fixture:    "workflow_run_in_progress.json",
			wantStatus: model.StatusStarted,
			wantTS:     time.Date(2026, 7, 13, 15, 34, 38, 0, time.UTC),
			wantActor:  "migueljfsc",
			wantRef:    "a43e0f0e815fbccb3fe0be2fba5085135ab7f7df",
			wantTitle:  "ci #4 in_progress (main)",
			wantDedup:  "gh:run:migueljfsc/wtc:29262668304:2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			var run restWorkflowRun
			loadFixture(t, tt.fixture, &run)
			ev, facts := NormalizeWorkflowRun(run, testNow)

			if err := ev.Validate(); err != nil {
				t.Fatalf("normalized event invalid: %v", err)
			}
			if ev.Source != model.SourceGitHub || ev.Kind != model.KindBuild {
				t.Errorf("source/kind = %s/%s", ev.Source, ev.Kind)
			}
			if ev.Status != tt.wantStatus {
				t.Errorf("status = %s, want %s", ev.Status, tt.wantStatus)
			}
			if !ev.TS.Equal(tt.wantTS) {
				t.Errorf("ts = %v, want %v", ev.TS, tt.wantTS)
			}
			switch {
			case tt.wantDuration == nil && ev.DurationMS != nil:
				t.Errorf("duration = %d, want nil", *ev.DurationMS)
			case tt.wantDuration != nil && (ev.DurationMS == nil || *ev.DurationMS != *tt.wantDuration):
				t.Errorf("duration = %v, want %d", ev.DurationMS, *tt.wantDuration)
			}
			if ev.Actor != tt.wantActor || ev.Ref != tt.wantRef || ev.Title != tt.wantTitle || ev.DedupKey != tt.wantDedup {
				t.Errorf("got actor=%q ref=%q title=%q dedup=%q", ev.Actor, ev.Ref, ev.Title, ev.DedupKey)
			}
			if ev.Env != "" {
				t.Errorf("env = %q — parser must not guess env (rules only)", ev.Env)
			}
			if facts.Repo != "migueljfsc/wtc" || facts.Event != "workflow_run" || !facts.PathsTruncated {
				t.Errorf("facts = %+v", facts)
			}
		})
	}

	// queued and in_progress share the dedup key with each other AND with
	// the eventual completed event — one row per attempt (trap #5).
	var q, p restWorkflowRun
	loadFixture(t, "workflow_run_queued.json", &q)
	loadFixture(t, "workflow_run_in_progress.json", &p)
	evQ, _ := NormalizeWorkflowRun(q, testNow)
	evP, _ := NormalizeWorkflowRun(p, testNow)
	if evQ.DedupKey != evP.DedupKey {
		t.Errorf("lifecycle dedup keys differ: %q vs %q", evQ.DedupKey, evP.DedupKey)
	}
}

func TestGoldenMergedPR(t *testing.T) {
	var pr restPullRequest
	loadFixture(t, "pull_request_merged.json", &pr)
	ev, facts := NormalizeMergedPR(pr, "migueljfsc/motorcycle-journey", testNow)
	if ev == nil {
		t.Fatal("merged PR must normalize")
	}
	if err := ev.Validate(); err != nil {
		t.Fatalf("invalid: %v", err)
	}
	if ev.Kind != model.KindMerge || ev.Status != model.StatusSucceeded {
		t.Errorf("kind/status = %s/%s", ev.Kind, ev.Status)
	}
	if !ev.TS.Equal(time.Date(2026, 6, 29, 1, 15, 46, 0, time.UTC)) {
		t.Errorf("ts = %v", ev.TS)
	}
	if ev.Ref != "359f482db01ac5361f30163d20a371e2ce150df2" {
		t.Errorf("ref = %q, want merge_commit_sha", ev.Ref)
	}
	if ev.Title != "PR #1 merged: chore(deps): Bump actions/checkout from 4 to 7" {
		t.Errorf("title = %q", ev.Title)
	}
	if ev.DedupKey != "gh:pr:migueljfsc/motorcycle-journey:1:merged" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if ev.Actor != "dependabot[bot]" {
		t.Errorf("actor = %q", ev.Actor)
	}
	if facts.Branch != "main" || facts.Event != "pull_request" || !facts.PathsTruncated {
		t.Errorf("facts = %+v", facts)
	}

	// Unmerged PR (merged_at null) must be dropped.
	pr.MergedAt = nil
	if ev, _ := NormalizeMergedPR(pr, "x/y", testNow); ev != nil {
		t.Error("unmerged PR must return nil")
	}
}

func TestGoldenCommit(t *testing.T) {
	var c restCommit
	loadFixture(t, "commit.json", &c)
	ev, facts := NormalizeCommit(c, "migueljfsc/wtc", testNow)
	if err := ev.Validate(); err != nil {
		t.Fatalf("invalid: %v", err)
	}
	if ev.Kind != model.KindPush || ev.Status != model.StatusSucceeded {
		t.Errorf("kind/status = %s/%s", ev.Kind, ev.Status)
	}
	if ev.Actor != "migueljfsc" {
		t.Errorf("actor = %q, want login over git author name", ev.Actor)
	}
	if ev.Ref != "f1945371e63a7556860fad1555be40a4a0d736a8" {
		t.Errorf("ref = %q", ev.Ref)
	}
	if ev.Title != "feat(github): phase 1 step 0 — capture mode, HMAC webhooks, poller skeleton" {
		t.Errorf("title = %q (first message line only)", ev.Title)
	}
	if ev.DedupKey != "gh:push:migueljfsc/wtc:f1945371e63a7556860fad1555be40a4a0d736a8" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if !ev.TS.Equal(time.Date(2026, 7, 13, 15, 11, 42, 0, time.UTC)) {
		t.Errorf("ts = %v, want committer date", ev.TS)
	}
	if !facts.PathsTruncated {
		t.Error("commit list has no files — PathsTruncated must be true")
	}
}

func TestGoldenEmptyResponses(t *testing.T) {
	var runs restWorkflowRunList
	loadFixture(t, "runs_empty.json", &runs)
	if len(runs.WorkflowRuns) != 0 {
		t.Errorf("empty runs fixture decoded %d runs", len(runs.WorkflowRuns))
	}
	var prs []restPullRequest
	loadFixture(t, "list_empty.json", &prs)
	if len(prs) != 0 {
		t.Errorf("empty list fixture decoded %d items", len(prs))
	}
}

func ptr[T any](v T) *T { return &v }
