package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/migueljfsc/wtc/internal/query"
)

// TestBlastE2E replays an incident through the real HTTP surface: events
// ingested via /ingest/generic, then /api/v1/blast anchored on the alert must
// rank the same-env deploy above an unrelated merge, and the reverse anchor
// (the deploy) must surface the alert that followed it.
func TestBlastE2E(t *testing.T) {
	ts := newTestServer(t)

	ingest := func(body string) string {
		t.Helper()
		resp, data := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, []byte(body))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("ingest = %d %s", resp.StatusCode, data)
		}
		var out IngestResponse
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		return out.ID
	}

	ingest(`{"kind":"merge","status":"succeeded","title":"unrelated docs PR",
		"ts":"2026-07-18T11:20:00Z","dedup_key":"e2e:merge"}`)
	deployID := ingest(`{"kind":"deploy","status":"succeeded","env":"prod","service":"api",
		"title":"deploy api sha-4f2a91c","ts":"2026-07-18T11:40:00Z","dedup_key":"e2e:deploy"}`)
	alertID := ingest(`{"kind":"alert","status":"failed","env":"prod","service":"api",
		"title":"HighErrorRate api","ts":"2026-07-18T12:00:00Z","dedup_key":"e2e:alert"}`)

	// Causes: the alert's top suspect is the same-env deploy, not the merge.
	resp, data := doRequest(t, http.MethodGet, ts.URL+"/api/v1/blast?id="+alertID, testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("blast = %d %s", resp.StatusCode, data)
	}
	var causes query.BlastReport
	if err := json.Unmarshal(data, &causes); err != nil {
		t.Fatal(err)
	}
	if causes.Direction != query.DirectionCauses {
		t.Fatalf("direction = %q, want causes", causes.Direction)
	}
	if len(causes.Suspects) != 2 || causes.Suspects[0].Event.ID != deployID {
		t.Fatalf("suspects = %+v, want the prod deploy ranked first", causes.Suspects)
	}
	if causes.Suspects[0].Score <= causes.Suspects[1].Score {
		t.Fatalf("deploy score %d not above merge score %d",
			causes.Suspects[0].Score, causes.Suspects[1].Score)
	}

	// Effects: anchored on the deploy, the subsequent alert comes back.
	resp, data = doRequest(t, http.MethodGet, ts.URL+"/api/v1/blast?id="+deployID, testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("blast effects = %d %s", resp.StatusCode, data)
	}
	var effects query.BlastReport
	if err := json.Unmarshal(data, &effects); err != nil {
		t.Fatal(err)
	}
	if effects.Direction != query.DirectionEffects {
		t.Fatalf("direction = %q, want effects", effects.Direction)
	}
	if len(effects.Suspects) != 1 || effects.Suspects[0].Event.ID != alertID {
		t.Fatalf("effects = %+v, want just the alert", effects.Suspects)
	}

	// Bare-ts anchor without env: 200, with the disabled-signal note.
	resp, data = doRequest(t, http.MethodGet,
		ts.URL+"/api/v1/blast?ts=2026-07-18T12:00:00Z", testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("blast ts = %d %s", resp.StatusCode, data)
	}
	var bare query.BlastReport
	if err := json.Unmarshal(data, &bare); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range bare.Notes {
		if strings.Contains(n, "same-env signal is disabled") {
			found = true
		}
	}
	if !found {
		t.Fatalf("bare-ts notes = %v, want the disabled-signal note", bare.Notes)
	}

	// Anchor validation: missing anchor 400, unknown id 404.
	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/blast", testToken, nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("no anchor = %d, want 400", resp.StatusCode)
	}
	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/blast?id=01AAAAAAAAAAAAAAAAAAAAAAAA", testToken, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown id = %d, want 404", resp.StatusCode)
	}
	// And auth: no token, no answer.
	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/blast?id="+alertID, "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", resp.StatusCode)
	}
}
