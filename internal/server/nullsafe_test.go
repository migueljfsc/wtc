package server

import (
	"net/http"
	"regexp"
	"testing"
)

// TestNoNullArraysInEmptyResponses pins the JSON contract for list-bearing
// fields: on an empty store, every field openapi types as a required array
// must marshal as [], never null. A nil Go slice here crashed the portal
// (`report.envs.length` on a sha with no applied envs) — this is the
// regression net for that whole class.
func TestNoNullArraysInEmptyResponses(t *testing.T) {
	ts := newTestServer(t)

	// Any `"field": null` is a contract break — required-array fields must be
	// []. Optional (omitempty) fields are absent, which is fine; a literal
	// null never is.
	nullField := regexp.MustCompile(`"[a-z_]+":\s*null`)

	paths := []string{
		// where on a sha nothing knows: builds/intents/envs all empty.
		"/api/v1/where/deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		// diff between envs with no deploys: rows empty.
		"/api/v1/diff?a=staging&b=prod",
		// empty-ledger facets: envs/services/actors/sources... empty.
		"/api/v1/facets",
		// empty-ledger doctor: sources empty.
		"/api/v1/doctor",
		// empty-ledger event list.
		"/api/v1/events",
		// handoff aggregates over nothing.
		"/api/v1/handoff?since=2020-01-01T00:00:00Z",
		// matrix over nothing.
		"/api/v1/matrix",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			resp, body := doRequest(t, http.MethodGet, ts.URL+path, testToken, nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d %s, want 200", resp.StatusCode, body)
			}
			if m := nullField.Find(body); m != nil {
				t.Errorf("empty-store response carries a null list field %s:\n%s", m, body)
			}
		})
	}
}
