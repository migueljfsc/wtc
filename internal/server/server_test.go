package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/migueljfsc/wtc/internal/store"
)

const testToken = "test-token"

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	srv := New(st, Options{Tokens: []string{testToken}}, slog.New(slog.DiscardHandler))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return ts
}

func doRequest(t *testing.T, method, url, token string, body []byte) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

func TestHealthzNoAuth(t *testing.T) {
	ts := newTestServer(t)
	resp, body := doRequest(t, http.MethodGet, ts.URL+"/healthz", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d %s, want 200", resp.StatusCode, body)
	}
}

func TestAuthRequired(t *testing.T) {
	ts := newTestServer(t)
	event := []byte(`{"kind":"manual","title":"x"}`)

	tests := []struct {
		name, method, path, token string
		body                      []byte
	}{
		{"ingest no token", http.MethodPost, "/ingest/generic", "", event},
		{"ingest wrong token", http.MethodPost, "/ingest/generic", "nope", event},
		{"events no token", http.MethodGet, "/api/events", "", nil},
		{"events wrong token", http.MethodGet, "/api/events", "nope", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, _ := doRequest(t, tt.method, ts.URL+tt.path, tt.token, tt.body)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

func TestIngestRoundTripAndDedup(t *testing.T) {
	ts := newTestServer(t)
	event := []byte(`{
		"kind": "manual", "env": "dev", "service": "api",
		"title": "test deploy note", "dedup_key": "e2e:1"
	}`)

	// First ingest: created.
	resp, body := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, event)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first ingest = %d %s, want 201", resp.StatusCode, body)
	}
	var first IngestResponse
	if err := json.Unmarshal(body, &first); err != nil {
		t.Fatal(err)
	}
	if first.Deduped || first.ID == "" {
		t.Fatalf("first ingest response = %+v", first)
	}

	// Same dedup_key again: deduped, same id, still one row.
	resp, body = doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, event)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("duplicate ingest = %d %s, want 200", resp.StatusCode, body)
	}
	var second IngestResponse
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatal(err)
	}
	if !second.Deduped || second.ID != first.ID {
		t.Fatalf("duplicate ingest response = %+v, want deduped onto %s", second, first.ID)
	}

	resp, body = doRequest(t, http.MethodGet, ts.URL+"/api/events?env=dev", testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list = %d %s", resp.StatusCode, body)
	}
	var list EventsResponse
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Events) != 1 {
		t.Fatalf("got %d events, want exactly 1 after duplicate ingest", len(list.Events))
	}
	got := list.Events[0]
	if got.Title != "test deploy note" || got.Service != "api" || string(got.Source) != "generic" {
		t.Fatalf("event mismatch: %+v", got)
	}
}

func TestIngestValidation(t *testing.T) {
	ts := newTestServer(t)
	tests := []struct {
		name string
		body string
	}{
		{"bad json", `{`},
		{"missing kind", `{"title":"x"}`},
		{"bad kind", `{"kind":"release","title":"x"}`},
		{"missing title", `{"kind":"manual"}`},
		{"bad ts", `{"kind":"manual","title":"x","ts":"yesterday"}`},
		{"bad status", `{"kind":"manual","title":"x","status":"done"}`},
		// generic ingest must not spoof dedicated ingest paths
		{"reserved source github", `{"kind":"build","title":"x","source":"github"}`},
		{"reserved source flux", `{"kind":"deploy","title":"x","source":"flux"}`},
		{"reserved dedup prefix gh", `{"kind":"build","title":"x","dedup_key":"gh:run:org/app:1:1"}`},
		{"reserved dedup prefix flux", `{"kind":"deploy","title":"x","dedup_key":"flux:prod:Kustomization/x:1:r"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, _ := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, []byte(tt.body))
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestListEventsBadParams(t *testing.T) {
	ts := newTestServer(t)
	for _, path := range []string{
		"/api/events?since=notatime",
		"/api/events?until=notatime",
		"/api/events?limit=-1",
		"/api/events?limit=abc",
		"/api/events?cursor=%21%21not-base64", // client input error, not 500
		"/api/events?q=payments",              // FTS is phase 3: reject, don't ignore
	} {
		resp, _ := doRequest(t, http.MethodGet, ts.URL+path, testToken, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", path, resp.StatusCode)
		}
	}
}

func TestIngestRedactsSecrets(t *testing.T) {
	ts := newTestServer(t)
	event := []byte(`{
		"kind": "manual", "env": "prod",
		"title": "hotfix: set password=hunter2 and used ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"artifacts": ["reg/app:v1"],
		"dedup_key": "e2e:redact"
	}`)
	resp, body := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, event)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("ingest = %d %s", resp.StatusCode, body)
	}

	_, body = doRequest(t, http.MethodGet, ts.URL+"/api/events?env=prod", testToken, nil)
	if bytes.Contains(body, []byte("hunter2")) || bytes.Contains(body, []byte("ghp_abcdefghijklmnopqrstuvwxyz")) {
		t.Fatalf("stored event still contains secrets: %s", body)
	}
	if !bytes.Contains(body, []byte("[REDACTED]")) {
		t.Fatalf("expected redaction placeholder in stored event: %s", body)
	}
	if !bytes.Contains(body, []byte("reg/app:v1")) {
		t.Fatalf("artifacts payload must survive redaction: %s", body)
	}
}

func TestDoctorEndpoint(t *testing.T) {
	ts := newTestServer(t)

	// Unauthenticated → 401.
	resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/doctor", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("doctor without token = %d, want 401", resp.StatusCode)
	}

	// Seed one unmapped event, then check the report.
	event := []byte(`{"kind":"manual","title":"orphan change","dedup_key":"e2e:doc1"}`)
	if resp, body := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, event); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed ingest = %d %s", resp.StatusCode, body)
	}

	resp, body := doRequest(t, http.MethodGet, ts.URL+"/api/doctor", testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("doctor = %d %s", resp.StatusCode, body)
	}
	var report struct {
		TotalEvents int64 `json:"total_events"`
		DBSizeBytes int64 `json:"db_size_bytes"`
		Sources     []struct {
			Source   string `json:"source"`
			Count24h int    `json:"count_24h"`
		} `json:"sources"`
		Unmapped24h     int      `json:"unmapped_24h"`
		UnmappedSamples []string `json:"unmapped_samples"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	if report.TotalEvents != 1 || report.DBSizeBytes <= 0 {
		t.Errorf("totals = %+v", report)
	}
	if len(report.Sources) != 1 || report.Sources[0].Source != "generic" || report.Sources[0].Count24h != 1 {
		t.Errorf("sources = %+v", report.Sources)
	}
	if report.Unmapped24h != 1 || len(report.UnmappedSamples) != 1 || report.UnmappedSamples[0] != "orphan change" {
		t.Errorf("unmapped = %d samples=%v, want the env-less event surfaced", report.Unmapped24h, report.UnmappedSamples)
	}
}

func TestFailClosedWithoutTokens(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	srv := httptest.NewServer(New(st, Options{}, slog.New(slog.DiscardHandler)).Handler())
	defer srv.Close()

	resp, _ := doRequest(t, http.MethodGet, srv.URL+"/api/events", "anything", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token server must fail closed, got %d", resp.StatusCode)
	}
}
