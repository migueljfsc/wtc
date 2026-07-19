package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// TestGrafanaLiveRoundTrip drives the real grafana-annotation sink against a
// real Grafana (P21 fixture discipline: the frozen payloads under
// testdata/grafana/ come from this test). Gated behind env vars — locally:
//
//	docker run -d --rm --name wtc-grafana -p 3300:3000 grafana/grafana:11.3.0
//	# create a service account (role Editor) + token, then:
//	WTC_TEST_GRAFANA_URL=http://127.0.0.1:3300 WTC_TEST_GRAFANA_TOKEN=glsa_… \
//	  go test ./internal/notify/ -run TestGrafanaLiveRoundTrip
//
// Set WTC_TEST_GRAFANA_CAPTURE to a directory to re-freeze the fixtures.
func TestGrafanaLiveRoundTrip(t *testing.T) {
	base := os.Getenv("WTC_TEST_GRAFANA_URL")
	token := os.Getenv("WTC_TEST_GRAFANA_TOKEN")
	if base == "" || token == "" {
		t.Skip("WTC_TEST_GRAFANA_URL / WTC_TEST_GRAFANA_TOKEN not set — skipping grafana live test")
	}
	captureDir := os.Getenv("WTC_TEST_GRAFANA_CAPTURE")

	// Recording reverse proxy: the sink talks to it, it forwards verbatim to
	// the real Grafana, and both sides of the round-trip are kept for the
	// fixture freeze.
	type roundTrip struct {
		reqBody  []byte
		respCode int
		respBody []byte
	}
	recorded := make(chan roundTrip, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody, _ := io.ReadAll(r.Body)
		out, err := http.NewRequestWithContext(r.Context(), r.Method,
			strings.TrimSuffix(base, "/")+r.URL.Path, bytes.NewReader(reqBody))
		if err != nil {
			t.Errorf("proxy build request: %v", err)
			return
		}
		out.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(out)
		if err != nil {
			t.Errorf("proxy forward: %v", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		respBody, _ := io.ReadAll(resp.Body)
		recorded <- roundTrip{reqBody: reqBody, respCode: resp.StatusCode, respBody: respBody}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
	}))
	defer proxy.Close()

	c, err := Compile([]Subscription{{
		Name: "grafana-live",
		Sink: Sink{Type: SinkGrafana, URL: proxy.URL, Token: token, Tags: []string{"wtc-live-test"}},
	}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	d := NewDispatcher(c, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	marker := fmt.Sprintf("wtc live %d", time.Now().UnixNano())
	ev := model.Event{
		ID: model.NewID(), TS: time.Now().UTC().Truncate(time.Millisecond),
		IngestedAt: time.Now().UTC(),
		Source:     model.SourceGeneric, Kind: model.KindDeploy,
		Status: model.StatusSucceeded, Env: "prod", Service: "api",
		Title: marker, URL: "https://ci.example.com/run/42", DedupKey: "live:" + marker,
	}
	d.Enqueue(ev, false)

	var rt roundTrip
	select {
	case rt = <-recorded:
	case <-time.After(10 * time.Second):
		t.Fatal("annotation never reached grafana")
	}
	if rt.respCode != http.StatusOK {
		t.Fatalf("grafana returned %d: %s", rt.respCode, rt.respBody)
	}

	// Read back through the annotations API: the write must be queryable, not
	// merely accepted.
	req, _ := http.NewRequest(http.MethodGet,
		strings.TrimSuffix(base, "/")+"/api/annotations?tags=wtc-live-test&limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var got []struct {
		Time int64    `json:"time"`
		Text string   `json:"text"`
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode annotations: %v", err)
	}
	found := false
	for _, a := range got {
		if strings.Contains(a.Text, marker) {
			found = true
			if a.Time != ev.TS.UnixMilli() {
				t.Errorf("annotation time = %d, want %d", a.Time, ev.TS.UnixMilli())
			}
		}
	}
	if !found {
		t.Fatalf("annotation with marker %q not found in read-back: %+v", marker, got)
	}

	if captureDir != "" {
		if err := os.MkdirAll(captureDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(captureDir, "annotation-request.json"), rt.reqBody, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(captureDir, "annotation-response.json"), rt.respBody, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
