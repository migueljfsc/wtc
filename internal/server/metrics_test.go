package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/migueljfsc/wtc/internal/metrics"
)

// TestMetricsRequiresAuth: /metrics leaks source names and activity levels, so
// it must be bearer-authed on the main listener (public-reachable posture).
func TestMetricsRequiresAuth(t *testing.T) {
	ts := newTestServer(t)
	resp, _ := doRequest(t, http.MethodGet, ts.URL+"/metrics", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /metrics = %d, want 401", resp.StatusCode)
	}
}

// TestMetricsIngestCounters is the accept criterion: N events ingested →
// counter moves by N; replaying them → the deduped counter moves instead.
// Counters are process-global (shared across tests), so every assertion is a
// delta.
func TestMetricsIngestCounters(t *testing.T) {
	ts := newTestServer(t)

	ingestedBefore := testutil.ToFloat64(metrics.Ingested.WithLabelValues("generic"))
	dedupedBefore := testutil.ToFloat64(metrics.Deduped.WithLabelValues("generic"))

	const n = 3
	for i := 0; i < n; i++ {
		event := fmt.Appendf(nil, `{"kind":"manual","env":"dev","service":"api","title":"m%d","dedup_key":"metrics:%d"}`, i, i)
		resp, body := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, event)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("ingest %d = %d %s, want 201", i, resp.StatusCode, body)
		}
	}
	if got := testutil.ToFloat64(metrics.Ingested.WithLabelValues("generic")) - ingestedBefore; got != n {
		t.Fatalf("wtc_ingested_total moved by %v, want %d", got, n)
	}

	// Replay: every event dedups onto its existing row.
	for i := 0; i < n; i++ {
		event := fmt.Appendf(nil, `{"kind":"manual","env":"dev","service":"api","title":"m%d","dedup_key":"metrics:%d"}`, i, i)
		resp, body := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, event)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("replay %d = %d %s, want 200", i, resp.StatusCode, body)
		}
	}
	if got := testutil.ToFloat64(metrics.Ingested.WithLabelValues("generic")) - ingestedBefore; got != n {
		t.Fatalf("wtc_ingested_total moved by %v after replay, want still %d", got, n)
	}
	if got := testutil.ToFloat64(metrics.Deduped.WithLabelValues("generic")) - dedupedBefore; got != n {
		t.Fatalf("wtc_deduped_total moved by %v, want %d", got, n)
	}
}

// TestMetricsExposition scrapes /metrics like Prometheus would and asserts the
// wtc instruments are present with the right label shapes — in particular that
// the HTTP histogram's path label is the route PATTERN, not the raw URL.
func TestMetricsExposition(t *testing.T) {
	ts := newTestServer(t)

	// Generate one routed request (pattern label) and one ingest.
	event := []byte(`{"kind":"manual","env":"dev","title":"scrape me","dedup_key":"metrics:scrape"}`)
	if resp, body := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, event); resp.StatusCode != http.StatusCreated {
		t.Fatalf("ingest = %d %s, want 201", resp.StatusCode, body)
	}
	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/events?env=dev", testToken, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("events = %d, want 200", resp.StatusCode)
	}

	resp, body := doRequest(t, http.MethodGet, ts.URL+"/metrics", testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scrape = %d, want 200", resp.StatusCode)
	}
	text := string(body)

	for _, want := range []string{
		`wtc_ingested_total{source="generic"}`,
		// newTestServer wires a sqlite store; the gauge samples it at scrape.
		`wtc_db_size_bytes{backend="sqlite"}`,
		// Route pattern, never the raw /api/v1/events?env=dev URL.
		`path="/api/v1/events"`,
		`path="/ingest/generic"`,
		`wtc_sse_connections 0`,
		// Runtime collectors registered alongside our instruments.
		"go_goroutines",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("scrape output missing %q", want)
		}
	}
	if strings.Contains(text, `path="/api/v1/events?env=dev"`) {
		t.Error("histogram path label contains a raw URL — cardinality trap")
	}
}
