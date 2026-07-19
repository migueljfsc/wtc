package server

import (
	stdcsv "encoding/csv"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/store"
)

// TestExplainE2E: a flux fixture ingested through the real webhook path (rules
// engine included) must explain its env/service as rule-derived; an event
// ingested via /ingest/generic (no engine) must honestly report no facts.
func TestExplainE2E(t *testing.T) {
	ts, st := newFluxTestServer(t, 10*time.Minute)

	body := fluxFixture(t, "kustomization_reconcile_succeeded.json")
	if resp, rbody := postFlux(t, ts.URL, body, fluxSign(body)); resp.StatusCode != http.StatusCreated {
		t.Fatalf("flux ingest = %d %s", resp.StatusCode, rbody)
	}
	events, _, err := st.ListEvents(t.Context(), store.Filter{})
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %d err=%v", len(events), err)
	}

	resp, data := doRequest(t, http.MethodGet, ts.URL+"/api/v1/explain/"+events[0].ID, testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("explain = %d %s", resp.StatusCode, data)
	}
	var rep ExplainReport
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	if !rep.Recorded {
		t.Fatalf("facts_recorded = false: %s", data)
	}
	byField := map[string]struct {
		origin, value string
		idx           *int
	}{}
	for _, tr := range rep.Traces {
		byField[tr.Field] = struct {
			origin, value string
			idx           *int
		}{tr.Origin, tr.Value, tr.RuleIndex}
	}
	if env := byField["env"]; env.origin != "rule" || env.value != "dev" || env.idx == nil || *env.idx != 0 {
		t.Errorf("env trace = %+v, want rule 0 → dev", byField["env"])
	}
	if svc := byField["service"]; svc.origin != "rule" || svc.value != "podinfo" || svc.idx == nil || *svc.idx != 1 {
		t.Errorf("service trace = %+v, want rule 1 → podinfo", byField["service"])
	}
	// No drift: the rules have not changed since ingest.
	for _, n := range rep.Notes {
		if strings.Contains(n, "changed since ingest") {
			t.Errorf("unexpected drift note: %s", n)
		}
	}

	// A generic event never runs the engine — no facts, and no guessing.
	resp, data = doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken,
		[]byte(`{"kind":"manual","title":"hand-rolled","env":"prod","dedup_key":"e2e:manual"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("generic ingest = %d %s", resp.StatusCode, data)
	}
	var ing IngestResponse
	if err := json.Unmarshal(data, &ing); err != nil {
		t.Fatal(err)
	}
	resp, data = doRequest(t, http.MethodGet, ts.URL+"/api/v1/explain/"+ing.ID, testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("explain generic = %d %s", resp.StatusCode, data)
	}
	var generic ExplainReport
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatal(err)
	}
	if generic.Recorded || len(generic.Notes) == 0 || !strings.Contains(generic.Notes[0], "facts not recorded") {
		t.Fatalf("generic explain = %s, want facts_recorded=false + note", data)
	}

	// Unknown id → 404.
	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/explain/nope", testToken, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown id = %d, want 404", resp.StatusCode)
	}
}

func seedExport(t *testing.T, ts string) {
	t.Helper()
	for _, body := range []string{
		`{"kind":"deploy","status":"succeeded","env":"prod","service":"api","title":"deploy api","ts":"2026-07-01T10:00:00Z","dedup_key":"x:1"}`,
		`{"kind":"deploy","status":"failed","env":"staging","service":"api","title":"deploy api stg","ts":"2026-07-01T11:00:00Z","dedup_key":"x:2"}`,
		`{"kind":"merge","status":"succeeded","env":"prod","service":"","title":"merge pr","ts":"2026-07-01T12:00:00Z","dedup_key":"x:3"}`,
	} {
		resp, data := doRequest(t, http.MethodPost, ts+"/ingest/generic", testToken, []byte(body))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed = %d %s", resp.StatusCode, data)
		}
	}
}

func TestExportE2E(t *testing.T) {
	ts := newTestServer(t)
	seedExport(t, ts.URL)

	// CSV: stable header, env filter applied, newest first.
	resp, data := doRequest(t, http.MethodGet, ts.URL+"/api/v1/export?env=prod&format=csv", testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export csv = %d %s", resp.StatusCode, data)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Errorf("content-type = %q", got)
	}
	rows, err := stdcsv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 { // header + 2 prod rows
		t.Fatalf("csv rows = %d, want 3: %q", len(rows), data)
	}
	if got := strings.Join(rows[0], ","); got != strings.Join(exportColumns, ",") {
		t.Errorf("csv header = %q — the column order is a stable contract", got)
	}
	if rows[1][14] != "merge pr" || rows[2][14] != "deploy api" {
		t.Errorf("csv rows out of order or misfiltered: %v", rows[1:])
	}

	// NDJSON: one full event per line.
	resp, data = doRequest(t, http.MethodGet, ts.URL+"/api/v1/export?format=ndjson", testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export ndjson = %d", resp.StatusCode)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("ndjson lines = %d, want 3", len(lines))
	}
	var ev model.Event
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil || ev.Title == "" {
		t.Fatalf("ndjson line does not decode to an event: %v %s", err, lines[0])
	}

	// JSON: a decodable array.
	resp, data = doRequest(t, http.MethodGet, ts.URL+"/api/v1/export?format=json&kind=deploy", testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export json = %d", resp.StatusCode)
	}
	var arr []model.Event
	if err := json.Unmarshal(data, &arr); err != nil || len(arr) != 2 {
		t.Fatalf("json export = %v (%d events), want 2 deploys", err, len(arr))
	}

	// Guardrails: bad format 400, auth enforced.
	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/export?format=xml", testToken, nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad format = %d, want 400", resp.StatusCode)
	}
	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/export", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", resp.StatusCode)
	}
}

// TestBackupE2E: the streamed snapshot must open as a valid sqlite ledger
// with identical events while the source server keeps serving.
func TestBackupE2E(t *testing.T) {
	ts := newTestServer(t)
	seedExport(t, ts.URL)

	resp, data := doRequest(t, http.MethodGet, ts.URL+"/api/v1/backup", testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("backup = %d %s", resp.StatusCode, data)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("content-type = %q", got)
	}

	snap := t.TempDir() + "/snapshot.db"
	if err := os.WriteFile(snap, data, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(snap)
	if err != nil {
		t.Fatalf("snapshot does not open as a wtc db: %v", err)
	}
	defer func() { _ = st.Close() }()
	events, _, err := st.ListEvents(t.Context(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("snapshot events = %d, want 3", len(events))
	}

	// The live server kept serving through and after the snapshot.
	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/events", testToken, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("live server after backup = %d", resp.StatusCode)
	}

	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/backup", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", resp.StatusCode)
	}
}
