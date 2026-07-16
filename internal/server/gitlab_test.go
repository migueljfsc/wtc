package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

const testGitLabToken = "gitlab-token"

// newGitLabTestServer wires a dev-overlay path rule so the webhook e2e
// exercises env inference on the ingested events.
func newGitLabTestServer(t *testing.T, token string) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := normalize.NewEngine([]normalize.Rule{
		{Match: normalize.RuleMatch{Source: "gitlab", Paths: []string{"**/overlays/dev/**"}}, Set: normalize.RuleSet{Env: "dev"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, Options{
		Tokens:             []string{testToken},
		GitLabWebhookToken: token,
		Engine:             normalize.NewEngineHolder(engine),
	}, slog.New(slog.DiscardHandler))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return ts, st
}

func gitlabFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../testdata/gitlab/webhook", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func gitlabPost(t *testing.T, url string, body []byte, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/ingest/gitlab", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// A valid X-Gitlab-Token stores the pipeline event; env inference and dedup
// keys converge with the poller path.
func TestGitLabWebhookPipeline(t *testing.T) {
	ts, st := newGitLabTestServer(t, testGitLabToken)
	resp := gitlabPost(t, ts.URL, gitlabFixture(t, "pipeline_pending.json"), map[string]string{
		"X-Gitlab-Token": testGitLabToken,
		"X-Gitlab-Event": "Pipeline Hook",
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	events, _, err := st.ListEvents(context.Background(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].DedupKey != "gl:pipeline:migueljfsc/wtc-demo-gitlab:2682902640" {
		t.Fatalf("events = %+v", events)
	}
}

// Replaying the same delivery twice leaves one row (webhook loss is recoverable
// by redelivery; the poller sweeps the same key idempotently).
func TestGitLabWebhookReplayDedups(t *testing.T) {
	ts, st := newGitLabTestServer(t, testGitLabToken)
	hdr := map[string]string{"X-Gitlab-Token": testGitLabToken, "X-Gitlab-Event": "Merge Request Hook"}
	body := gitlabFixture(t, "merge_request_merged.json")

	r1 := gitlabPost(t, ts.URL, body, hdr)
	_ = r1.Body.Close()
	if r1.StatusCode != http.StatusCreated {
		t.Fatalf("first status = %d, want 201", r1.StatusCode)
	}
	r2 := gitlabPost(t, ts.URL, body, hdr)
	_ = r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("replay status = %d, want 200 (all deduped)", r2.StatusCode)
	}
	events, _, err := st.ListEvents(context.Background(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d rows after replay, want 1", len(events))
	}
}

// A root-only push lands with env="" (matches no overlay path) — the honest
// unmapped case doctor surfaces, never a guess.
func TestGitLabWebhookPushUnknownEnv(t *testing.T) {
	ts, st := newGitLabTestServer(t, testGitLabToken)
	resp := gitlabPost(t, ts.URL, gitlabFixture(t, "push_root_only.json"), map[string]string{
		"X-Gitlab-Token": testGitLabToken,
		"X-Gitlab-Event": "Push Hook",
	})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	events, _, err := st.ListEvents(context.Background(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d rows, want 1", len(events))
	}
	if events[0].Env != "" {
		t.Errorf("env = %q, want empty", events[0].Env)
	}
}

// Auth: bad token → 401, no token → 401, unconfigured secret → 503.
func TestGitLabWebhookAuth(t *testing.T) {
	ts, _ := newGitLabTestServer(t, testGitLabToken)
	body := gitlabFixture(t, "pipeline_pending.json")

	bad := gitlabPost(t, ts.URL, body, map[string]string{"X-Gitlab-Token": "wrong"})
	_ = bad.Body.Close()
	if bad.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad token status = %d, want 401", bad.StatusCode)
	}
	none := gitlabPost(t, ts.URL, body, nil)
	_ = none.Body.Close()
	if none.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token status = %d, want 401", none.StatusCode)
	}

	tsOff, _ := newGitLabTestServer(t, "")
	off := gitlabPost(t, tsOff.URL, body, map[string]string{"X-Gitlab-Token": "anything"})
	_ = off.Body.Close()
	if off.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("unconfigured status = %d, want 503", off.StatusCode)
	}
}
