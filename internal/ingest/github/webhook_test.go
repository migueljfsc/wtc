package github

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/migueljfsc/wtc/internal/model"
)

const webhookDir = "../../../testdata/github/webhook"

func readWebhook(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(webhookDir, name))
	if err != nil {
		t.Fatalf("read webhook fixture %s: %v", name, err)
	}
	return raw
}

// Golden: a workflow_run(completed, success) delivery → one build Event whose
// dedup key matches what the poller derives for the same run id+attempt — the
// convergence that lets the poller sweep webhook losses.
func TestGoldenWebhookWorkflowRun(t *testing.T) {
	pairs, err := ParseWebhook("workflow_run", readWebhook(t, "workflow_run_completed_success.json"), testNow)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("got %d events, want 1", len(pairs))
	}
	ev := pairs[0].Event
	if ev.Source != model.SourceGitHub || ev.Kind != model.KindBuild {
		t.Errorf("source/kind = %s/%s", ev.Source, ev.Kind)
	}
	if ev.Status != model.StatusSucceeded {
		t.Errorf("status = %q, want succeeded", ev.Status)
	}
	if ev.DedupKey != "gh:run:migueljfsc/wtc:29534601016:1" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if ev.Ref != "64af1204817fd71e442f365ae464a2f2c12a0d8e" {
		t.Errorf("ref = %q", ev.Ref)
	}
	// run_started_at 21:04:05 → updated_at 21:04:41 = 36s.
	if ev.DurationMS == nil || *ev.DurationMS != 36_000 {
		t.Errorf("duration = %v, want 36000", ev.DurationMS)
	}
	if ev.Title != "ci #85 success (wtc-cap-feat)" {
		t.Errorf("title = %q", ev.Title)
	}
	if err := ev.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

// Golden: a real failed workflow_run delivery → failed build Event.
func TestGoldenWebhookWorkflowRunFailure(t *testing.T) {
	pairs, err := ParseWebhook("workflow_run", readWebhook(t, "workflow_run_completed_failure.json"), testNow)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("got %d events, want 1", len(pairs))
	}
	if pairs[0].Event.Status != model.StatusFailed {
		t.Errorf("status = %q, want failed", pairs[0].Event.Status)
	}
	if pairs[0].Event.DedupKey != "gh:run:migueljfsc/wtc:29540088375:1" {
		t.Errorf("dedup = %q", pairs[0].Event.DedupKey)
	}
}

// Golden: a push delivery → one push Event per commit with known paths (env
// inference can run), dedup coalescing with the poller's commit list.
func TestGoldenWebhookPush(t *testing.T) {
	pairs, err := ParseWebhook("push", readWebhook(t, "push.json"), testNow)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("got %d events, want 1", len(pairs))
	}
	ev, facts := pairs[0].Event, pairs[0].Facts
	if ev.Kind != model.KindPush {
		t.Errorf("kind = %s", ev.Kind)
	}
	if ev.DedupKey != "gh:push:migueljfsc/wtc:64af1204817fd71e442f365ae464a2f2c12a0d8e" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if ev.Actor != "migueljfsc" {
		t.Errorf("actor = %q", ev.Actor)
	}
	if facts.PathsTruncated {
		t.Error("push webhook carries the commit file set — paths are known, not truncated")
	}
	if len(facts.Paths) != 1 || facts.Paths[0] != ".wtc-webhook-capture" {
		t.Errorf("paths = %v", facts.Paths)
	}
}

// A ping (or any unmodeled event) yields no events and no error.
func TestWebhookUnhandledEvent(t *testing.T) {
	pairs, err := ParseWebhook("ping", []byte(`{"zen":"hi"}`), testNow)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 0 {
		t.Errorf("ping yielded %d events, want 0", len(pairs))
	}
}

// Golden: a real merged-PR delivery → one merge Event, ts = merged_at, ref =
// merge commit sha, dedup converging with the poller's gh:pr key.
func TestGoldenWebhookPullRequestMerged(t *testing.T) {
	pairs, err := ParseWebhook("pull_request", readWebhook(t, "pull_request_merged.json"), testNow)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("got %d events, want 1", len(pairs))
	}
	ev := pairs[0].Event
	if ev.Kind != model.KindMerge || ev.Status != model.StatusSucceeded {
		t.Errorf("kind/status = %s/%s", ev.Kind, ev.Status)
	}
	if ev.Ref != "d300213dd59d92076d3867cc481a84deb9759353" {
		t.Errorf("ref = %q (want merge commit sha)", ev.Ref)
	}
	if ev.DedupKey != "gh:pr:migueljfsc/wtc:9:merged" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if got := model.FormatTS(ev.TS); got != "2026-07-17T07:52:26.000Z" {
		t.Errorf("ts = %s (want merged_at)", got)
	}
	if err := ev.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

// Golden: a real opened-PR delivery decodes but yields no event — only a merge
// is a change intent. Guards the envelope decode + drop path against a real
// body.
func TestGoldenWebhookPullRequestOpenedDropped(t *testing.T) {
	pairs, err := ParseWebhook("pull_request", readWebhook(t, "pull_request_opened.json"), testNow)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 0 {
		t.Errorf("opened PR yielded %d events, want 0", len(pairs))
	}
}

// A closed-but-unmerged PR is likewise not a change intent.
func TestWebhookPullRequestClosedUnmergedDropped(t *testing.T) {
	body := []byte(`{"action":"closed","pull_request":{"number":1,"merged":false},"repository":{"full_name":"o/r"}}`)
	pairs, err := ParseWebhook("pull_request", body, testNow)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 0 {
		t.Errorf("closed-unmerged PR yielded %d events, want 0", len(pairs))
	}
}
