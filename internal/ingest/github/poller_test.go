package github

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

// fakeGitHub serves the frozen fixtures as list responses, mimicking the
// three REST endpoints the poller hits.
func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	read := func(name string) json.RawMessage {
		raw, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	runs, _ := json.Marshal(map[string]any{
		"total_count": 2,
		"workflow_runs": []json.RawMessage{
			read("workflow_run_completed_success.json"),
			read("workflow_run_completed_failure.json"),
		},
	})
	prs, _ := json.Marshal([]json.RawMessage{read("pull_request_merged.json")})
	commits, _ := json.Marshal([]json.RawMessage{read("commit.json")})

	mux := http.NewServeMux()
	serveRaw := func(body []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		}
	}
	repos, _ := json.Marshal([]map[string]any{
		{"full_name": "migueljfsc/wtc", "archived": false},
		{"full_name": "migueljfsc/archived", "archived": true}, // skipped
	})
	mux.HandleFunc("GET /user/repos", serveRaw(repos))
	mux.HandleFunc("GET /repos/migueljfsc/wtc/actions/runs", serveRaw(runs))
	mux.HandleFunc("GET /repos/migueljfsc/wtc/pulls", serveRaw(prs))
	mux.HandleFunc("GET /repos/migueljfsc/wtc/pulls/1/files", serveRaw(read("pull_request_files.json")))
	mux.HandleFunc("GET /repos/migueljfsc/wtc/commits", serveRaw(commits))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestPollerAutoDiscoversRepos: with no repos configured, the poller lists the
// token's accessible repos and polls them (archived ones skipped).
func TestPollerAutoDiscoversRepos(t *testing.T) {
	gh := fakeGitHub(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	old := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, res := range []string{"runs", "prs", "commits"} {
		if err := st.SetPollWatermark(ctx, "migueljfsc/wtc", res, old); err != nil {
			t.Fatal(err)
		}
	}

	engine, _ := normalize.NewEngine(nil)
	// nil repos => auto-discover.
	p := NewPoller(NewClient("test-token", gh.URL), st, normalize.NewEngineHolder(engine),
		nil, time.Minute, "", slog.New(slog.DiscardHandler))
	p.Sweep(ctx)

	events, _, err := st.ListEvents(ctx, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	// Discovered migueljfsc/wtc (archived repo skipped) and polled its 4 events.
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4 from the discovered repo", len(events))
	}
}

func TestPollerSweepIngestsAndDedups(t *testing.T) {
	gh := fakeGitHub(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	engine, err := normalize.NewEngine([]normalize.Rule{
		{Match: normalize.RuleMatch{Source: "github", Event: "workflow_run"},
			Set: normalize.RuleSet{Service: `{{ trimPrefix .Repo "migueljfsc/" }}`}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Fixtures are frozen in time: pin the watermark clock by seeding
	// watermarks far in the past so `since` filtering keeps everything.
	ctx := context.Background()
	old := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, res := range []string{"runs", "prs", "commits"} {
		if err := st.SetPollWatermark(ctx, "migueljfsc/wtc", res, old); err != nil {
			t.Fatal(err)
		}
	}

	p := NewPoller(NewClient("test-token", gh.URL), st, normalize.NewEngineHolder(engine),
		[]string{"migueljfsc/wtc"}, time.Minute, "", slog.New(slog.DiscardHandler))

	p.Sweep(ctx)

	events, _, err := st.ListEvents(ctx, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	// 2 runs + 1 merged PR + 1 commit.
	if len(events) != 4 {
		for _, ev := range events {
			t.Logf("  %s %s %s", ev.Kind, ev.Status, ev.DedupKey)
		}
		t.Fatalf("got %d events, want 4", len(events))
	}

	byDedup := map[string]model.Event{}
	for _, ev := range events {
		byDedup[ev.DedupKey] = ev
	}
	run, ok := byDedup["gh:run:migueljfsc/wtc:29261201471:1"]
	if !ok || run.Status != model.StatusSucceeded {
		t.Errorf("success run missing/wrong: %+v", run)
	}
	if run.Service != "wtc" {
		t.Errorf("service = %q — rules engine must run in the poller path", run.Service)
	}
	if fail := byDedup["gh:run:migueljfsc/wtc:29211857530:1"]; fail.Status != model.StatusFailed {
		t.Errorf("failure run status = %s", fail.Status)
	}
	if _, ok := byDedup["gh:pr:migueljfsc/motorcycle-journey:1:merged"]; !ok {
		t.Error("merged PR missing (repo from base.repo.full_name)")
	}
	if _, ok := byDedup["gh:push:migueljfsc/wtc:f1945371e63a7556860fad1555be40a4a0d736a8"]; !ok {
		t.Error("commit missing")
	}

	// Watermarks advanced to the newest source ts per resource.
	wm, err := st.PollWatermark(ctx, "migueljfsc/wtc", "runs")
	if err != nil || !wm.After(old) {
		t.Errorf("runs watermark = %v err=%v, want advanced", wm, err)
	}

	// Second sweep: same payloads, zero new rows (poller is idempotent —
	// this is what makes it double as the webhook-loss sweeper).
	p.Sweep(ctx)
	events, _, err = st.ListEvents(ctx, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("after second sweep: %d events, want 4 (idempotent re-ingest)", len(events))
	}
}

func TestPollerSurvivesAPIFailure(t *testing.T) {
	// A repo whose endpoints 500 must not stop other repos' ingest.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	bad := httptest.NewServer(mux)
	defer bad.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	engine, _ := normalize.NewEngine(nil)

	p := NewPoller(NewClient("t", bad.URL), st, normalize.NewEngineHolder(engine),
		[]string{"migueljfsc/wtc"}, time.Minute, "", slog.New(slog.DiscardHandler))
	p.Sweep(context.Background()) // must not panic or wedge

	// Watermark untouched on failure — nothing lost.
	wm, err := st.PollWatermark(context.Background(), "migueljfsc/wtc", "runs")
	if err != nil || !wm.IsZero() {
		t.Errorf("watermark = %v err=%v, want zero (only advances on success)", wm, err)
	}
}
