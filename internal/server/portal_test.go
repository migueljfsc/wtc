package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/migueljfsc/wtc/internal/store"
)

// newTestStore opens a throwaway store closed on cleanup.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return st
}

// newHTTPTest starts an httptest server for srv, closed on cleanup, and returns
// its base URL.
func newHTTPTest(t *testing.T, srv *Server) string {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts.URL
}

// doWithHeaders issues a request with arbitrary headers set.
func doWithHeaders(t *testing.T, method, url string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
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
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

// TestAPIVersionedAliasParity checks that every query route answers identically
// under the legacy /api prefix and the versioned /api/v1 prefix.
func TestAPIVersionedAliasParity(t *testing.T) {
	ts := newTestServer(t)
	for _, path := range []string{"/events", "/doctor", "/auth/verify"} {
		legacy, _ := doRequest(t, http.MethodGet, ts.URL+"/api"+path, testToken, nil)
		v1, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1"+path, testToken, nil)
		if legacy.StatusCode != http.StatusOK || v1.StatusCode != http.StatusOK {
			t.Fatalf("%s: /api=%d /api/v1=%d, want both 200", path, legacy.StatusCode, v1.StatusCode)
		}
		// Both prefixes must enforce auth.
		if noauth, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1"+path, "", nil); noauth.StatusCode != http.StatusUnauthorized {
			t.Fatalf("/api/v1%s without token = %d, want 401", path, noauth.StatusCode)
		}
	}
}

func TestAuthVerify(t *testing.T) {
	ts := newTestServer(t)

	resp, body := doRequest(t, http.MethodGet, ts.URL+"/api/v1/auth/verify", testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify with token = %d %s, want 200", resp.StatusCode, body)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(body, &out); err != nil || !out.OK {
		t.Fatalf("verify body = %s (err %v), want {ok:true}", body, err)
	}

	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/auth/verify", "wrong", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("verify with bad token = %d, want 401", resp.StatusCode)
	}
}

func TestOpenAPIServed(t *testing.T) {
	ts := newTestServer(t)
	// Public, like healthz — no token needed.
	resp, body := doRequest(t, http.MethodGet, ts.URL+"/api/openapi.json", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("openapi = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("openapi content-type = %q, want application/json", ct)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("openapi is not valid JSON: %v", err)
	}
	if doc["openapi"] == nil || doc["paths"] == nil {
		t.Fatalf("openapi doc missing top-level fields: %v", doc)
	}
}

// TestOpenAPINoDrift asserts every registered /api/v1 route is documented in
// the embedded spec — the guard that a new endpoint cannot ship without its
// contract.
func TestOpenAPINoDrift(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Options{Tokens: []string{testToken}}, slog.New(slog.DiscardHandler))

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(openapiJSON, &doc); err != nil {
		t.Fatalf("parse embedded openapi.json: %v", err)
	}
	for _, rt := range srv.apiRoutes() {
		key := "/api/v1" + rt.path
		methods, ok := doc.Paths[key]
		if !ok {
			t.Errorf("route %s %s is not documented in openapi.json (path %q missing)", rt.method, key, key)
			continue
		}
		if _, ok := methods[strings.ToLower(rt.method)]; !ok {
			t.Errorf("route %s %s is not documented (method %s missing under %q)", rt.method, key, rt.method, key)
		}
	}
}

func TestStatsEndpoints(t *testing.T) {
	ts := newTestServer(t)

	// Auth is enforced.
	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/stats/deploys", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stats without token = %d, want 401", resp.StatusCode)
	}
	// Bad bucket is a 400, not a 500.
	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/stats/activity?bucket=week", testToken, nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad bucket = %d, want 400", resp.StatusCode)
	}

	// Seed a successful prod deploy, then it should surface in both endpoints.
	dep := []byte(`{"kind":"deploy","env":"prod","service":"api","status":"succeeded","title":"deploy api","dedup_key":"stats:1"}`)
	if resp, body := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, dep); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d %s", resp.StatusCode, body)
	}

	resp, body := doRequest(t, http.MethodGet, ts.URL+"/api/v1/stats/deploys", testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("deploys = %d %s", resp.StatusCode, body)
	}
	var dstats struct {
		Envs []struct {
			Env        string `json:"env"`
			Total      int    `json:"total"`
			Succeeded  int    `json:"succeeded"`
			LastStatus string `json:"last_status"`
		} `json:"envs"`
	}
	if err := json.Unmarshal(body, &dstats); err != nil {
		t.Fatal(err)
	}
	if len(dstats.Envs) != 1 || dstats.Envs[0].Env != "prod" || dstats.Envs[0].Total != 1 || dstats.Envs[0].LastStatus != "succeeded" {
		t.Fatalf("deploy stats = %+v, want one prod deploy", dstats.Envs)
	}

	resp, body = doRequest(t, http.MethodGet, ts.URL+"/api/v1/stats/activity?bucket=day", testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("activity = %d %s", resp.StatusCode, body)
	}
	var astats struct {
		Bucket  string           `json:"bucket"`
		Buckets []map[string]any `json:"buckets"`
	}
	if err := json.Unmarshal(body, &astats); err != nil {
		t.Fatal(err)
	}
	if astats.Bucket != "day" || len(astats.Buckets) == 0 {
		t.Fatalf("activity stats = %+v, want non-empty day buckets", astats)
	}
}

func TestFacetsAndActorFilter(t *testing.T) {
	ts := newTestServer(t)

	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/facets", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("facets without token = %d, want 401", resp.StatusCode)
	}

	for _, ev := range []string{
		`{"kind":"deploy","env":"prod","service":"api","actor":"alice","title":"a","dedup_key":"fa:1"}`,
		`{"kind":"deploy","env":"dev","service":"web","actor":"bob","title":"b","dedup_key":"fa:2"}`,
	} {
		if resp, body := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, []byte(ev)); resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed = %d %s", resp.StatusCode, body)
		}
	}

	_, body := doRequest(t, http.MethodGet, ts.URL+"/api/v1/facets", testToken, nil)
	var facets struct {
		Envs, Services, Actors []string
	}
	if err := json.Unmarshal(body, &facets); err != nil {
		t.Fatal(err)
	}
	if len(facets.Envs) != 2 || len(facets.Services) != 2 || len(facets.Actors) != 2 {
		t.Fatalf("facets = %+v, want 2 of each", facets)
	}

	// actor= filters to exact match.
	_, body = doRequest(t, http.MethodGet, ts.URL+"/api/v1/events?actor=alice", testToken, nil)
	var list EventsResponse
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Events) != 1 || list.Events[0].Actor != "alice" {
		t.Fatalf("actor filter returned %d events, want 1 alice", len(list.Events))
	}
}

func TestCORS(t *testing.T) {
	const allowed = "https://portal.example.com"

	corsServer := func(t *testing.T, origins ...string) string {
		st := newTestStore(t)
		srv := New(st, Options{Tokens: []string{testToken}, CORSAllowedOrigins: origins}, slog.New(slog.DiscardHandler))
		ts := newHTTPTest(t, srv)
		return ts
	}

	t.Run("preflight from allowed origin", func(t *testing.T) {
		url := corsServer(t, allowed)
		resp, _ := doWithHeaders(t, http.MethodOptions, url+"/api/v1/events", map[string]string{"Origin": allowed})
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("preflight = %d, want 204", resp.StatusCode)
		}
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != allowed {
			t.Fatalf("ACAO = %q, want %q", got, allowed)
		}
		if !strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "Authorization") {
			t.Fatalf("preflight must allow the Authorization header, got %q", resp.Header.Get("Access-Control-Allow-Headers"))
		}
	})

	t.Run("actual request carries ACAO even on 401", func(t *testing.T) {
		url := corsServer(t, allowed)
		// No token → 401, but CORS headers must still be present so the browser
		// can read the error.
		resp, _ := doWithHeaders(t, http.MethodGet, url+"/api/v1/events", map[string]string{"Origin": allowed})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != allowed {
			t.Fatalf("ACAO = %q, want %q", got, allowed)
		}
	})

	t.Run("disallowed origin gets no ACAO", func(t *testing.T) {
		url := corsServer(t, allowed)
		resp, _ := doWithHeaders(t, http.MethodGet, url+"/api/v1/events", map[string]string{"Origin": "https://evil.example.com"})
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("ACAO = %q, want empty for a disallowed origin", got)
		}
	})

	t.Run("wildcard allows any origin", func(t *testing.T) {
		url := corsServer(t, "*")
		other := "https://anything.example.com"
		resp, _ := doWithHeaders(t, http.MethodGet, url+"/api/v1/events", map[string]string{"Origin": other})
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != other {
			t.Fatalf("ACAO = %q, want the echoed origin %q under wildcard", got, other)
		}
	})

	t.Run("cors off emits no headers", func(t *testing.T) {
		url := corsServer(t) // no origins
		resp, _ := doWithHeaders(t, http.MethodGet, url+"/api/v1/events", map[string]string{"Origin": allowed})
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("ACAO = %q, want empty when CORS is off", got)
		}
	})
}
