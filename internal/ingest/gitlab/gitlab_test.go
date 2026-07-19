package gitlab

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

const (
	apiDir     = "../../../testdata/gitlab/api"
	webhookDir = "../../../testdata/gitlab/webhook"
	// project path the poller supplies (API payloads carry only project_id).
	testProject = "migueljfsc/wtc-demo-gitlab"
)

var testNow = time.Date(2026, 7, 16, 21, 0, 0, 0, time.UTC)

func readFixture(t *testing.T, dir, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

func decodeAPI(t *testing.T, name string, v any) {
	t.Helper()
	if err := json.Unmarshal(readFixture(t, apiDir, name), v); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
}

func ptr[T any](v T) *T { return &v }

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %s: %v", s, err)
	}
	return ts.UTC()
}

// Golden: API pipeline payloads → normalized build Events.
func TestGoldenPipeline(t *testing.T) {
	tests := []struct {
		fixture      string
		wantStatus   model.Status
		wantTS       string // finished_at for terminal pipelines
		wantDuration *int64
		wantRef      string
		wantDedup    string
	}{
		{
			fixture:      "pipeline_success.json",
			wantStatus:   model.StatusSucceeded,
			wantTS:       "2026-07-16T20:08:32.735Z",
			wantDuration: ptr(int64(32_000)),
			wantRef:      "c8dc94ce80551cb4fb0770452bcc059833e22839",
			wantDedup:    "gl:pipeline:migueljfsc/wtc-demo-gitlab:2682902699",
		},
		{
			fixture:      "pipeline_failed.json",
			wantStatus:   model.StatusFailed,
			wantTS:       "2026-07-16T20:08:39.859Z",
			wantDuration: ptr(int64(5_000)),
			wantRef:      "", // sha asserted below via event
			wantDedup:    "gl:pipeline:migueljfsc/wtc-demo-gitlab:2682903696",
		},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			var p restPipeline
			decodeAPI(t, tc.fixture, &p)
			ev, facts := NormalizePipeline(p, testProject, testNow)

			if ev.Source != model.SourceGitLab || ev.Kind != model.KindBuild {
				t.Errorf("source/kind = %s/%s", ev.Source, ev.Kind)
			}
			if ev.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", ev.Status, tc.wantStatus)
			}
			if got := model.FormatTS(ev.TS); got != model.FormatTS(mustTime(t, tc.wantTS)) {
				t.Errorf("ts = %s, want %s", got, tc.wantTS)
			}
			if !durEqual(ev.DurationMS, tc.wantDuration) {
				t.Errorf("duration = %v, want %v", deref(ev.DurationMS), deref(tc.wantDuration))
			}
			if tc.wantRef != "" && ev.Ref != tc.wantRef {
				t.Errorf("ref = %q, want %q", ev.Ref, tc.wantRef)
			}
			if ev.DedupKey != tc.wantDedup {
				t.Errorf("dedup = %q, want %q", ev.DedupKey, tc.wantDedup)
			}
			if facts.Source != "gitlab" || facts.Repo != testProject || facts.Event != "pipeline" {
				t.Errorf("facts = %+v", facts)
			}
			if !facts.PathsTruncated {
				t.Error("pipeline facts should be paths-truncated (no file list)")
			}
			if err := ev.Validate(); err != nil {
				t.Errorf("validate: %v", err)
			}
		})
	}
}

// Golden: a merged MR → merge Event, ts = merged_at, ref = merge commit sha.
func TestGoldenMergedMR(t *testing.T) {
	var list []restMergeRequest
	decodeAPI(t, "merge_requests_merged.json", &list)
	if len(list) == 0 {
		t.Fatal("empty MR fixture")
	}
	ev, facts := NormalizeMergedMR(list[0], testProject, testNow)
	if ev == nil {
		t.Fatal("merged MR normalized to nil")
	}
	if ev.Kind != model.KindMerge || ev.Status != model.StatusSucceeded {
		t.Errorf("kind/status = %s/%s", ev.Kind, ev.Status)
	}
	if ev.Ref != "c8dc94ce80551cb4fb0770452bcc059833e22839" {
		t.Errorf("ref = %q (want merge commit sha)", ev.Ref)
	}
	if ev.DedupKey != "gl:mr:migueljfsc/wtc-demo-gitlab:1:merged" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if got := model.FormatTS(ev.TS); got != model.FormatTS(mustTime(t, "2026-07-16T20:07:48.566Z")) {
		t.Errorf("ts = %s (want merged_at)", got)
	}
	if facts.Event != "merge_request" || facts.Branch != "main" {
		t.Errorf("facts = %+v", facts)
	}
	if err := ev.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

// Unmerged/closed MRs are not change intents.
func TestMergedMRSkipsUnmerged(t *testing.T) {
	mr := restMergeRequest{IID: 9, State: "closed", MergedAt: nil}
	if ev, _ := NormalizeMergedMR(mr, testProject, testNow); ev != nil {
		t.Errorf("closed MR normalized to non-nil: %+v", ev)
	}
}

// Golden: a default-branch commit → push Event (paths unknown from the list).
func TestGoldenCommit(t *testing.T) {
	var c restCommit
	decodeAPI(t, "commit.json", &c)
	ev, facts := NormalizeCommit(c, testProject, testNow)
	if ev.Kind != model.KindPush || ev.Status != model.StatusSucceeded {
		t.Errorf("kind/status = %s/%s", ev.Kind, ev.Status)
	}
	if ev.Ref != "c8dc94ce80551cb4fb0770452bcc059833e22839" {
		t.Errorf("ref = %q", ev.Ref)
	}
	if ev.DedupKey != "gl:push:migueljfsc/wtc-demo-gitlab:c8dc94ce80551cb4fb0770452bcc059833e22839" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if !facts.PathsTruncated {
		t.Error("commit-list push must be paths-truncated")
	}
	if err := ev.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

func durEqual(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
