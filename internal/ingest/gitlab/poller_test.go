package gitlab

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

// fakeGitLab serves the frozen fixtures as the poller's REST endpoints:
// pipelines list (+ per-pipeline detail), merged MRs (+ changes), commits.
func fakeGitLab(t *testing.T) *httptest.Server {
	t.Helper()
	read := func(dir, name string) json.RawMessage { return readFixture(t, dir, name) }
	serveRaw := func(body []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
		}
	}

	// A two-item pipeline list; detail is served per id.
	pipelines, _ := json.Marshal([]json.RawMessage{
		read(apiDir, "pipeline_success.json"),
		read(apiDir, "pipeline_failed.json"),
	})
	mrs := read(apiDir, "merge_requests_merged.json")
	commits := read(apiDir, "commits.json")

	const enc = "migueljfsc%2Fwtc-demo-gitlab"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/projects/"+enc+"/pipelines", serveRaw(pipelines))
	mux.HandleFunc("GET /api/v4/projects/"+enc+"/pipelines/2682902699", serveRaw(read(apiDir, "pipeline_success.json")))
	mux.HandleFunc("GET /api/v4/projects/"+enc+"/pipelines/2682903696", serveRaw(read(apiDir, "pipeline_failed.json")))
	mux.HandleFunc("GET /api/v4/projects/"+enc+"/merge_requests", serveRaw(mrs))
	mux.HandleFunc("GET /api/v4/projects/"+enc+"/merge_requests/1/changes", serveRaw(read(apiDir, "merge_request_changes.json")))
	mux.HandleFunc("GET /api/v4/projects/"+enc+"/repository/commits", serveRaw(commits))
	// Encoded path uses %2F; Go's mux decodes it, so also register decoded form.
	mux.HandleFunc("GET /api/v4/projects/migueljfsc/wtc-demo-gitlab/pipelines", serveRaw(pipelines))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newEngine(t *testing.T) *normalize.EngineHolder {
	t.Helper()
	e, err := normalize.NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	return normalize.NewEngineHolder(e)
}

// Two identical sweeps over the same fixtures must leave exactly one row per
// dedup key: the poller is the idempotent webhook-loss sweeper.
func TestPollerTwiceIsIdempotent(t *testing.T) {
	gl := fakeGitLab(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	client := NewClient("t", gl.URL)
	p := NewPoller(client, st, newEngine(t), []string{testProject}, time.Minute, 0, "", slog.New(slog.NewTextHandler(nopWriter{}, nil)))

	ctx := context.Background()
	p.Sweep(ctx)
	after1 := len(listAll(t, st))
	p.Sweep(ctx)
	after2 := len(listAll(t, st))

	if after1 == 0 {
		t.Fatal("first sweep stored nothing")
	}
	if after1 != after2 {
		t.Errorf("row count changed across sweeps: %d → %d (not idempotent)", after1, after2)
	}
	// Expected distinct logical changes: 2 pipelines + 1 MR + commits.
	if after1 < 3 {
		t.Errorf("stored %d rows, expected at least 3 (2 pipelines + 1 MR)", after1)
	}
}

// The merged-MR row carries the enrichment payload (image bump) from the
// changes API — the tag↔revision link survives the full poller path.
func TestPollerEnrichesMR(t *testing.T) {
	gl := fakeGitLab(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	p := NewPoller(NewClient("t", gl.URL), st, newEngine(t), []string{testProject}, time.Minute, 0, "", slog.New(slog.NewTextHandler(nopWriter{}, nil)))
	p.Sweep(context.Background())

	var found bool
	for _, ev := range listAll(t, st) {
		if ev.DedupKey == "gl:mr:migueljfsc/wtc-demo-gitlab:1:merged" {
			found = true
			if !strings.Contains(ev.Payload, "sha-190b65d7") {
				t.Errorf("MR payload missing image bump: %q", ev.Payload)
			}
		}
	}
	if !found {
		t.Fatal("merged MR row not found")
	}
}

func listAll(t *testing.T, st *store.Store) []model.Event {
	t.Helper()
	events, _, err := st.ListEvents(context.Background(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
