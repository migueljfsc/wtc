package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/migueljfsc/wtc/internal/metrics"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

const testFluxKey = "flux-hmac-key"

func newFluxTestServer(t *testing.T, suppression time.Duration) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine, err := normalize.NewEngine([]normalize.Rule{
		{Match: normalize.RuleMatch{Source: "flux", Cluster: "dev"}, Set: normalize.RuleSet{Env: "dev"}},
		{Match: normalize.RuleMatch{Source: "flux", ObjectKind: "Kustomization"}, Set: normalize.RuleSet{Service: "{{ .ObjectName }}"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, Options{
		Tokens:          []string{testToken},
		FluxHMACKey:     testFluxKey,
		FluxSuppression: suppression,
		Engine:          normalize.NewEngineHolder(engine),
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

func fluxSign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(testFluxKey))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func fluxFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../testdata/flux", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func postFlux(t *testing.T, url string, body []byte, sig string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/ingest/flux", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if sig != "" {
		req.Header.Set("X-Signature", sig)
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

func TestFluxIngestEndToEnd(t *testing.T) {
	ts, st := newFluxTestServer(t, 10*time.Minute)
	body := fluxFixture(t, "kustomization_reconcile_succeeded.json")
	suppressedBefore := testutil.ToFloat64(metrics.Suppressed.WithLabelValues("flux"))

	// Bad signature → 401, nothing stored.
	resp, _ := postFlux(t, ts.URL, body, "sha256=deadbeef")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad sig = %d, want 401", resp.StatusCode)
	}
	resp, _ = postFlux(t, ts.URL, body, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no sig = %d, want 401", resp.StatusCode)
	}

	// Valid → 201, event stored with rules applied.
	resp, rbody := postFlux(t, ts.URL, body, fluxSign(body))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("valid sig = %d %s, want 201", resp.StatusCode, rbody)
	}

	events, _, err := st.ListEvents(t.Context(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Env != "dev" {
		t.Errorf("env = %q — cluster:dev rule must map to env dev", ev.Env)
	}
	if ev.Service != "podinfo" {
		t.Errorf("service = %q — ObjectName rule must apply", ev.Service)
	}

	// Immediate replay → suppressed by the window, still one row.
	resp, rbody = postFlux(t, ts.URL, body, fluxSign(body))
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
	// P16: the shed replay must move the suppression counter (delta — the
	// counter is process-global).
	if got := testutil.ToFloat64(metrics.Suppressed.WithLabelValues("flux")) - suppressedBefore; got != 1 {
		t.Errorf("wtc_suppressed_total{source=\"flux\"} moved by %v, want 1", got)
	}
}

func TestFluxSuppressionDisabledStillDedups(t *testing.T) {
	// Even with the window off (0), N identical events store exactly 1 row —
	// the strict-rank upsert is the correctness backstop (trap #1).
	ts, st := newFluxTestServer(t, 0)
	body := fluxFixture(t, "kustomization_artifact_failed.json")

	for range 4 {
		resp, rbody := postFlux(t, ts.URL, body, fluxSign(body))
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Fatalf("ingest = %d %s", resp.StatusCode, rbody)
		}
	}
	events, _, err := st.ListEvents(t.Context(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("4 identical reconcile failures stored %d rows, want 1", len(events))
	}
	if events[0].Status != "failed" {
		t.Errorf("status = %s", events[0].Status)
	}
}

func TestFluxNotConfiguredFailsClosed(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	srv := httptest.NewServer(New(st, Options{}, slog.New(slog.DiscardHandler)).Handler())
	defer srv.Close()

	body := []byte(`{}`)
	resp, _ := postFlux(t, srv.URL, body, fluxSign(body))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured = %d, want 503", resp.StatusCode)
	}
}
