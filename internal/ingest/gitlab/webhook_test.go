package gitlab

import (
	"testing"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
)

// Golden: a pipeline webhook (pending) → one started build Event whose dedup
// key matches what the API path derives for the same pipeline id — the
// convergence that lets the poller sweep webhook losses.
func TestGoldenWebhookPipeline(t *testing.T) {
	pairs, err := ParseWebhook(readFixture(t, webhookDir, "pipeline_pending.json"), testNow)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("got %d events, want 1", len(pairs))
	}
	ev := pairs[0].Event
	if ev.Status != model.StatusStarted {
		t.Errorf("status = %q, want started", ev.Status)
	}
	if ev.DedupKey != "gl:pipeline:migueljfsc/wtc-demo-gitlab:2682902640" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if ev.DurationMS != nil {
		t.Errorf("pending pipeline should carry no duration, got %d", *ev.DurationMS)
	}
	if err := ev.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

// Golden: a merge-action MR webhook → one merge Event; dedup converges with the
// API MR path (same project + iid).
func TestGoldenWebhookMergedMR(t *testing.T) {
	pairs, err := ParseWebhook(readFixture(t, webhookDir, "merge_request_merged.json"), testNow)
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
	if ev.Ref != "c8dc94ce80551cb4fb0770452bcc059833e22839" {
		t.Errorf("ref = %q (want merge commit sha)", ev.Ref)
	}
	if ev.DedupKey != "gl:mr:migueljfsc/wtc-demo-gitlab:1:merged" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
}

// A non-merge MR action (open/update/approved) is dropped, not errored.
func TestWebhookMRNonMergeDropped(t *testing.T) {
	body := []byte(`{"object_kind":"merge_request","object_attributes":{"iid":1,"action":"open"},"project":{"path_with_namespace":"g/s"}}`)
	pairs, err := ParseWebhook(body, testNow)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 0 {
		t.Errorf("open MR yielded %d events, want 0", len(pairs))
	}
}

// Golden: a push touching only a root file (FAIL) yields a push
// Event with known-but-non-matching paths, so env inference lands at "".
func TestGoldenWebhookPushUnknownEnv(t *testing.T) {
	pairs, err := ParseWebhook(readFixture(t, webhookDir, "push_root_only.json"), testNow)
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
	if ev.DedupKey != "gl:push:migueljfsc/wtc-demo-gitlab:fc4f6eb9e2b20994cdd143d93d2057ddbf39d6cc" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if facts.PathsTruncated {
		t.Error("push webhook carries the commit file set — paths are known, not truncated")
	}
	if len(facts.Paths) != 1 || facts.Paths[0] != "FAIL" {
		t.Errorf("paths = %v, want [FAIL]", facts.Paths)
	}

	// Run through a dev-overlay path rule: it must NOT match → env stays "".
	eng := devEnvEngine(t)
	if err := eng.Apply(ev, facts); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if ev.Env != "" {
		t.Errorf("env = %q, want \"\" (root-only push matches no env path)", ev.Env)
	}
}

// A push webhook with N commits yields N push events (one per commit).
func TestWebhookPushPerCommit(t *testing.T) {
	body := []byte(`{"object_kind":"push","user_username":"u","project":{"path_with_namespace":"g/s"},"commits":[
		{"id":"aaa","title":"one","timestamp":"2026-07-16T20:00:00Z","modified":["a"]},
		{"id":"bbb","title":"two","timestamp":"2026-07-16T20:01:00Z","added":["b"]}]}`)
	pairs, err := ParseWebhook(body, testNow)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("got %d events, want 2", len(pairs))
	}
	if pairs[0].Event.DedupKey != "gl:push:g/s:aaa" || pairs[1].Event.DedupKey != "gl:push:g/s:bbb" {
		t.Errorf("dedup keys = %q, %q", pairs[0].Event.DedupKey, pairs[1].Event.DedupKey)
	}
}

// devEnvEngine builds a rules engine with a single dev-overlay path rule, the
// realistic env-inference shape (kustomize overlays under infrastructure/).
func devEnvEngine(t *testing.T) *normalize.Engine {
	t.Helper()
	eng, err := normalize.NewEngine([]normalize.Rule{{
		Match: normalize.RuleMatch{Source: "gitlab", Paths: []string{"**/overlays/dev/**"}},
		Set:   normalize.RuleSet{Env: "dev"},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	return eng
}
