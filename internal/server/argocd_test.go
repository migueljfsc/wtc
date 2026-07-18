package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/migueljfsc/wtc/internal/metrics"
	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/query"
	"github.com/migueljfsc/wtc/internal/store"
)

const testArgoCDToken = "argocd-token"

// newArgoCDTestServer wires the default argocd inference rules (SPEC §2:
// env label > destination namespace > app-name suffix) so the e2e tests
// exercise the shipped configuration, not a synthetic one.
func newArgoCDTestServer(t *testing.T, captureDir string, suppression time.Duration) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := normalize.NewEngine([]normalize.Rule{
		{Match: normalize.RuleMatch{Source: "argocd"}, Set: normalize.RuleSet{Env: "{{ .EnvLabel }}"}},
		{Match: normalize.RuleMatch{Source: "argocd", Namespace: "staging"}, Set: normalize.RuleSet{Env: "staging"}},
		{Match: normalize.RuleMatch{Source: "argocd", Namespace: "prod"}, Set: normalize.RuleSet{Env: "prod"}},
		{Match: normalize.RuleMatch{Source: "argocd", ObjectName: "*-staging"}, Set: normalize.RuleSet{Env: "staging", Service: `{{ trimSuffix .ObjectName "-staging" }}`}},
		{Match: normalize.RuleMatch{Source: "argocd"}, Set: normalize.RuleSet{Service: "{{ .ObjectName }}"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, Options{
		Tokens:             []string{testToken},
		ArgoCDWebhookToken: testArgoCDToken,
		ArgoCDSuppression:  suppression,
		Engine:             normalize.NewEngineHolder(engine),
		CaptureDir:         captureDir,
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

func argocdFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../testdata/argocd", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func argocdPost(t *testing.T, url string, body []byte, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(body)))
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
	defer func() { _ = resp.Body.Close() }()
	data := make([]byte, 4096)
	n, _ := resp.Body.Read(data)
	return resp, data[:n]
}

func withArgoToken(body []byte) (b []byte, h map[string]string) {
	return body, map[string]string{"X-WTC-Token": testArgoCDToken, "Content-Type": "application/json"}
}

func TestArgoCDToken(t *testing.T) {
	ts, _ := newArgoCDTestServer(t, "", 0)
	body := argocdFixture(t, "sync_succeeded.json")
	url := ts.URL + "/ingest/argocd"

	tests := []struct {
		name  string
		token string
		want  int
	}{
		{"wrong token", "wrong-token", http.StatusUnauthorized},
		{"missing header", "", http.StatusUnauthorized},
		{"valid token", testArgoCDToken, http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{}
			if tt.token != "" {
				headers["X-WTC-Token"] = tt.token
			}
			resp, rbody := argocdPost(t, url, body, headers)
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d %s, want %d", resp.StatusCode, rbody, tt.want)
			}
		})
	}
}

func TestArgoCDScopeDropsUnlisted(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Whitelist only a payments app; the fixture app (wtc-guestbook-labeled)
	// is outside it and must be dropped before storage.
	scope, err := normalize.ScopeFilter{
		Allow: []normalize.ScopeMatch{{ObjectName: "payments-*"}},
	}.Compile()
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, Options{
		Tokens:             []string{testToken},
		ArgoCDWebhookToken: testArgoCDToken,
		ArgoCDScope:        scope,
	}, slog.New(slog.DiscardHandler))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	filteredBefore := testutil.ToFloat64(metrics.Filtered.WithLabelValues("argocd"))
	body, headers := withArgoToken(argocdFixture(t, "sync_succeeded.json"))
	resp, rbody := argocdPost(t, ts.URL+"/ingest/argocd", body, headers)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("filtered = %d %s, want 202", resp.StatusCode, rbody)
	}
	var out map[string]string
	if err := json.Unmarshal(rbody, &out); err != nil || out["status"] != "filtered" {
		t.Fatalf("response = %s, want status filtered", rbody)
	}
	events, _, err := st.ListEvents(t.Context(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("unlisted app stored %d rows, want 0", len(events))
	}
	if got := testutil.ToFloat64(metrics.Filtered.WithLabelValues("argocd")) - filteredBefore; got != 1 {
		t.Errorf("wtc_filtered_total{source=\"argocd\"} moved by %v, want 1", got)
	}
}

func TestArgoCDWebhookNotConfiguredFailsClosed(t *testing.T) {
	// Server without a webhook secret must reject even a token-bearing request.
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	srv := httptest.NewServer(New(st, Options{Tokens: []string{testToken}}, slog.New(slog.DiscardHandler)).Handler())
	defer srv.Close()

	resp, _ := argocdPost(t, srv.URL+"/ingest/argocd", []byte(`{}`), map[string]string{
		"X-WTC-Token": testArgoCDToken,
	})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured webhook endpoint = %d, want 503", resp.StatusCode)
	}
}

func TestArgoCDRejectsUnparseable(t *testing.T) {
	ts, st := newArgoCDTestServer(t, "", 0)
	// Authenticated but not the canonical shape (no app): 400, nothing stored.
	body, headers := withArgoToken([]byte(`{"project":"default","syncStatus":"Synced"}`))
	resp, _ := argocdPost(t, ts.URL+"/ingest/argocd", body, headers)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unparseable = %d, want 400", resp.StatusCode)
	}
	events, _, _ := st.ListEvents(t.Context(), store.Filter{})
	if len(events) != 0 {
		t.Fatalf("stored %d rows, want 0", len(events))
	}
}

func TestArgoCDCapture(t *testing.T) {
	dir := t.TempDir()
	ts, _ := newArgoCDTestServer(t, dir, 0)
	body, headers := withArgoToken(argocdFixture(t, "sync_succeeded.json"))

	resp, rbody := argocdPost(t, ts.URL+"/ingest/argocd", body, headers)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d %s, want 201", resp.StatusCode, rbody)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "argocd", "*wtc-guestbook-labeled-Succeeded.json"))
	if err != nil || len(matches) != 1 {
		entries, _ := os.ReadDir(filepath.Join(dir, "argocd"))
		t.Fatalf("captured files = %v (err %v), dir contents: %v", matches, err, entries)
	}
	captured, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(captured) != string(body) {
		t.Fatalf("captured body mismatch: %s", captured)
	}

	// The shared secret itself must never be captured to disk.
	headerFiles, _ := filepath.Glob(filepath.Join(dir, "argocd", "*.headers"))
	if len(headerFiles) != 1 {
		t.Fatalf("expected 1 headers sidecar, got %v", headerFiles)
	}
	headerData, err := os.ReadFile(headerFiles[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(headerData), testArgoCDToken) {
		t.Fatalf("captured headers leaked the shared secret: %s", headerData)
	}
}

// mutateArgoCDBody unmarshals a captured fixture, applies field overrides,
// and re-marshals — used to derive same-operation or retry variants the
// capture session didn't produce (fixture-mutation precedent: flux tests).
func mutateArgoCDBody(t *testing.T, fixture string, overrides map[string]any) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(argocdFixture(t, fixture), &m); err != nil {
		t.Fatal(err)
	}
	for k, v := range overrides {
		m[k] = v
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestArgoCDLifecycleEndToEnd replays one sync operation's Running→Succeeded
// transition (same startedAt — the running leg is derived from the succeeded
// fixture, since the captured pair came from different operations): one row,
// status transitions in place (trap #5), rules applied (env from the app's
// env label), suppression not tripped across distinct phases.
func TestArgoCDLifecycleEndToEnd(t *testing.T) {
	ts, st := newArgoCDTestServer(t, "", 10*time.Minute)
	url := ts.URL + "/ingest/argocd"

	running := mutateArgoCDBody(t, "sync_succeeded.json", map[string]any{
		"operationPhase": "Running", "finishedAt": nil,
	})
	body, headers := withArgoToken(running)
	resp, rbody := argocdPost(t, url, body, headers)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("running = %d %s, want 201", resp.StatusCode, rbody)
	}

	body, headers = withArgoToken(argocdFixture(t, "sync_succeeded.json"))
	resp, rbody = argocdPost(t, url, body, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("succeeded = %d %s, want 200 (dedup onto the running row)", resp.StatusCode, rbody)
	}

	events, _, err := st.ListEvents(t.Context(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("lifecycle stored %d rows, want 1", len(events))
	}
	ev := events[0]
	if ev.Status != "succeeded" {
		t.Errorf("status = %s — the transition must upsert in place", ev.Status)
	}
	if ev.Env != "staging" {
		t.Errorf("env = %q — the env label tier must apply", ev.Env)
	}
	if ev.Service != "wtc-guestbook-labeled" {
		t.Errorf("service = %q", ev.Service)
	}

	// Resync spam: an identical replay is shed by the window, still one row.
	resp, rbody = argocdPost(t, url, body, headers)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("replay = %d %s, want 202 suppressed", resp.StatusCode, rbody)
	}
	var sup map[string]string
	if err := json.Unmarshal(rbody, &sup); err != nil || sup["status"] != "suppressed" {
		t.Fatalf("replay response = %s", rbody)
	}
	events, _, _ = st.ListEvents(t.Context(), store.Filter{})
	if len(events) != 1 {
		t.Fatalf("after replay: %d rows, want 1", len(events))
	}
}

// TestArgoCDSuppressionDisabledStillDedups mirrors the flux backstop test:
// with the window off, N identical notifications still store exactly 1 row.
func TestArgoCDSuppressionDisabledStillDedups(t *testing.T) {
	ts, st := newArgoCDTestServer(t, "", 0)
	body, headers := withArgoToken(argocdFixture(t, "sync_succeeded.json"))

	for range 4 {
		resp, rbody := argocdPost(t, ts.URL+"/ingest/argocd", body, headers)
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Fatalf("ingest = %d %s", resp.StatusCode, rbody)
		}
	}
	events, _, err := st.ListEvents(t.Context(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("4 identical notifications stored %d rows, want 1", len(events))
	}
}

// TestArgoCDRetryVisibleAsTwoRows: a Failed sync then a Succeeded retry of
// the same revision are distinct operations and the ledger must show BOTH
// attempts — with an (app,revision)-only key the row froze at failed forever
// (equal terminal ranks never overwrite; found live in stage 3).
func TestArgoCDRetryVisibleAsTwoRows(t *testing.T) {
	ts, st := newArgoCDTestServer(t, "", 10*time.Minute)
	url := ts.URL + "/ingest/argocd"

	failed := mutateArgoCDBody(t, "sync_succeeded.json", map[string]any{
		"operationPhase": "Failed",
		"startedAt":      "2026-07-16T10:19:00Z",
		"finishedAt":     "2026-07-16T10:19:01Z",
	})
	body, headers := withArgoToken(failed)
	resp, rbody := argocdPost(t, url, body, headers)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("failed sync = %d %s, want 201", resp.StatusCode, rbody)
	}

	body, headers = withArgoToken(argocdFixture(t, "sync_succeeded.json"))
	resp, rbody = argocdPost(t, url, body, headers)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("retry = %d %s, want 201 (a NEW row, not an upsert)", resp.StatusCode, rbody)
	}

	events, _, err := st.ListEvents(t.Context(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("retry stored %d rows, want 2 (both attempts visible)", len(events))
	}
	statuses := map[model.Status]bool{events[0].Status: true, events[1].Status: true}
	if !statuses[model.StatusFailed] || !statuses[model.StatusSucceeded] {
		t.Errorf("statuses = %v, want one failed and one succeeded", statuses)
	}
}

// TestArgoCDDegradedAndWhere: a completed sync followed by a health
// degradation upserts the SAME operation's row to degraded (the degraded
// body carries the previous sync's startedAt), and the where join sees the
// argocd deploy as APPLIED — exactly like a flux reconcile revision.
func TestArgoCDDegradedAndWhere(t *testing.T) {
	ts, st := newArgoCDTestServer(t, "", 10*time.Minute)
	url := ts.URL + "/ingest/argocd"

	// The completed sync this degradation refers to: same operation identity
	// (app/revision/startedAt) as health_degraded.json, healthy at the time.
	completed := mutateArgoCDBody(t, "health_degraded.json", map[string]any{
		"syncStatus": "Synced", "healthStatus": "Healthy",
	})
	body, headers := withArgoToken(completed)
	resp, rbody := argocdPost(t, url, body, headers)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("sync = %d %s, want 201", resp.StatusCode, rbody)
	}

	body, headers = withArgoToken(argocdFixture(t, "health_degraded.json"))
	resp, rbody = argocdPost(t, url, body, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("degraded = %d %s, want 200 (upsert onto the operation's row)", resp.StatusCode, rbody)
	}

	events, _, err := st.ListEvents(t.Context(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("degraded created a new row: %d rows, want 1", len(events))
	}
	if events[0].Status != model.StatusDegraded {
		t.Errorf("status = %s — degraded must outrank succeeded on the same row", events[0].Status)
	}

	// where: the sync revision is an APPLIED manifest revision.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/where/8088f4c0d970abb09e250248cc97e35623447cb5", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	wresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = wresp.Body.Close() }()
	if wresp.StatusCode != http.StatusOK {
		t.Fatalf("where = %d", wresp.StatusCode)
	}
	var report query.WhereReport
	if err := json.NewDecoder(wresp.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, we := range report.Envs {
		if we.Env == "prod" && we.Applied != nil && we.Applied.Source == model.SourceArgoCD {
			found = true
		}
	}
	if !found {
		t.Fatalf("where must report the argocd deploy as APPLIED in prod; envs = %+v", report.Envs)
	}
}
