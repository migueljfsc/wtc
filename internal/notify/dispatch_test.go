package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/migueljfsc/wtc/internal/metrics"
	"github.com/migueljfsc/wtc/internal/model"
)

func TestCompileValidation(t *testing.T) {
	tests := []struct {
		name    string
		sub     Subscription
		wantErr string
	}{
		{"slack ok", Subscription{Sink: Sink{Type: "slack", URL: "https://hooks.slack.com/x"}}, ""},
		{"webhook ok", Subscription{Sink: Sink{Type: "webhook", URL: "https://example.com/hook", Token: "t"}}, ""},
		{"grafana ok", Subscription{Sink: Sink{Type: "grafana-annotation", URL: "http://grafana:3000", Token: "t", Tags: []string{"deploys"}}}, ""},
		{"missing type", Subscription{Sink: Sink{URL: "https://x"}}, "type is required"},
		{"unknown type", Subscription{Sink: Sink{Type: "pagerduty", URL: "https://x"}}, "unknown type"},
		{"slack missing url", Subscription{Sink: Sink{Type: "slack"}}, "url"},
		{"slack stray token", Subscription{Sink: Sink{Type: "slack", URL: "https://x", Token: "t"}}, "do not apply"},
		{"webhook stray tags", Subscription{Sink: Sink{Type: "webhook", URL: "https://x", Tags: []string{"a"}}}, "do not apply"},
		{"grafana missing token", Subscription{Sink: Sink{Type: "grafana-annotation", URL: "http://x"}}, "token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile([]Subscription{tt.sub})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Compile: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Compile err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSubscriptionMatch(t *testing.T) {
	ev := model.Event{
		Env: "prod", Service: "api", Repo: "acme/api",
		Kind: model.KindDeploy, Status: model.StatusFailed,
	}
	tests := []struct {
		name  string
		match Match
		want  bool
	}{
		{"empty matches everything", Match{}, true},
		{"env exact", Match{Env: "prod"}, true},
		{"env glob", Match{Env: "pr*"}, true},
		{"env miss", Match{Env: "staging"}, false},
		{"kind+status", Match{Kind: "deploy", Status: "failed"}, true},
		{"status miss", Match{Kind: "deploy", Status: "succeeded"}, false},
		{"repo two-segment glob", Match{Repo: "acme/*"}, true},
		{"repo star is single-segment", Match{Repo: "*"}, false}, // acme/api has a /
		{"repo doublestar", Match{Repo: "**"}, true},
		{"service", Match{Service: "api"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Compile([]Subscription{{Match: tt.match, Sink: Sink{Type: "slack", URL: "https://x"}}})
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if got := c.subs[0].matches(ev); got != tt.want {
				t.Fatalf("matches = %v, want %v", got, tt.want)
			}
		})
	}
}

// notifyEvent is a stored-shape event (post-merge, no payload).
func notifyEvent() model.Event {
	return model.Event{
		ID: model.NewID(), TS: time.Now().UTC(), IngestedAt: time.Now().UTC(),
		Source: model.SourceGeneric, Kind: model.KindDeploy, Status: model.StatusSucceeded,
		Env: "prod", Service: "api", Title: "deploy api v1.2.3",
		URL: "https://ci.example.com/run/42", DedupKey: "t:notify",
	}
}

// testDispatcher builds a dispatcher with instant retries for one sink URL.
func testDispatcher(t *testing.T, sinkType, url string) *Dispatcher {
	t.Helper()
	c, err := Compile([]Subscription{{
		Name: "test-sub",
		Sink: Sink{Type: sinkType, URL: url, Token: tokenFor(sinkType)},
	}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	d := NewDispatcher(c, nil)
	d.backoff = []time.Duration{0, 0, 0}
	return d
}

func tokenFor(sinkType string) string {
	if sinkType == SinkGrafana {
		return "glsa_secret"
	}
	return ""
}

func TestDispatcherWebhookDelivery(t *testing.T) {
	got := make(chan WebhookPayload, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p WebhookPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("decode: %v", err)
		}
		got <- p
	}))
	defer srv.Close()

	d := testDispatcher(t, SinkWebhook, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	ev := notifyEvent()
	d.Enqueue(ev, true)

	select {
	case p := <-got:
		if p.Notification != "test-sub" || !p.Transition {
			t.Fatalf("payload = %+v, want test-sub/transition", p)
		}
		if p.Event.ID != ev.ID || p.Event.Env != "prod" {
			t.Fatalf("event = %+v", p.Event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("webhook delivery never arrived")
	}
}

func TestDispatcherRetriesThenDrops(t *testing.T) {
	droppedBefore := testutil.ToFloat64(metrics.NotifyDropped.WithLabelValues("test-sub", "webhook", "retries_exhausted"))

	var mu sync.Mutex
	attempts := 0
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		if n == 4 {
			close(done)
		}
	}))
	defer srv.Close()

	d := testDispatcher(t, SinkWebhook, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	d.Enqueue(notifyEvent(), false)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("want 4 attempts, got %d", attempts)
	}
	// The dropped counter moves after the last failed attempt returns.
	deadline := time.Now().Add(5 * time.Second)
	for testutil.ToFloat64(metrics.NotifyDropped.WithLabelValues("test-sub", "webhook", "retries_exhausted"))-droppedBefore != 1 {

		if time.Now().After(deadline) {
			t.Fatal("dropped counter never moved")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDispatcherQueueFullDrops(t *testing.T) {
	droppedBefore := testutil.ToFloat64(metrics.NotifyDropped.WithLabelValues("test-sub", "webhook", "queue_full"))

	d := testDispatcher(t, SinkWebhook, "http://127.0.0.1:0/never-called")
	// No Run(): the queue only fills. queueSize sends fit, one more drops.
	for i := 0; i < queueSize+3; i++ {
		d.Enqueue(notifyEvent(), false)
	}
	if got := testutil.ToFloat64(metrics.NotifyDropped.WithLabelValues("test-sub", "webhook", "queue_full")) - droppedBefore; got != 3 {
		t.Fatalf("queue_full drops = %v, want 3", got)
	}
}

func TestDispatcherSkipsNonMatching(t *testing.T) {
	c, err := Compile([]Subscription{{
		Name:  "prod-only",
		Match: Match{Env: "prod"},
		Sink:  Sink{Type: "webhook", URL: "http://127.0.0.1:0/never-called"},
	}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	d := NewDispatcher(c, nil)
	ev := notifyEvent()
	ev.Env = "staging"
	d.Enqueue(ev, false)
	if len(d.queue) != 0 {
		t.Fatal("non-matching event was queued")
	}
}

func TestGrafanaAnnotationRequest(t *testing.T) {
	type capture struct {
		path, auth string
		body       grafanaAnnotation
	}
	got := make(chan capture, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var a grafanaAnnotation
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			t.Errorf("decode: %v", err)
		}
		got <- capture{path: r.URL.Path, auth: r.Header.Get("Authorization"), body: a}
	}))
	defer srv.Close()

	d := testDispatcher(t, SinkGrafana, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	ev := notifyEvent()
	d.Enqueue(ev, false)

	select {
	case c := <-got:
		if c.path != "/api/annotations" {
			t.Fatalf("path = %s", c.path)
		}
		if c.auth != "Bearer glsa_secret" {
			t.Fatalf("auth = %q", c.auth)
		}
		if c.body.Time != ev.TS.UnixMilli() {
			t.Fatalf("time = %d, want %d", c.body.Time, ev.TS.UnixMilli())
		}
		want := []string{"wtc", "deploy", "env:prod", "service:api"}
		if len(c.body.Tags) != len(want) {
			t.Fatalf("tags = %v, want %v", c.body.Tags, want)
		}
		for i, tag := range want {
			if c.body.Tags[i] != tag {
				t.Fatalf("tags = %v, want %v", c.body.Tags, want)
			}
		}
		if !strings.Contains(c.body.Text, "deploy api v1.2.3") {
			t.Fatalf("text = %q", c.body.Text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("grafana delivery never arrived")
	}
}

func TestEventSlackText(t *testing.T) {
	ev := notifyEvent()
	ev.Title = "deploy <api> & co"
	text := eventSlackText(ev, true)
	for _, want := range []string{"*[prod]*", "deploy api", "→", "*succeeded*", "&lt;api&gt; &amp; co", "<https://ci.example.com/run/42|"} {
		if !strings.Contains(text, want) {
			t.Fatalf("slack text %q missing %q", text, want)
		}
	}
	if strings.Contains(eventSlackText(ev, false), "→") {
		t.Fatal("non-transition must not carry the arrow")
	}
}

// TestGrafanaFixtureShape locks the sink to the captured real round-trip
// (testdata/grafana/, Grafana 11.3.0): the frozen request must parse
// losslessly into the sink's own body type, and the frozen response is the
// accept shape the live test observed.
func TestGrafanaFixtureShape(t *testing.T) {
	reqRaw, err := os.ReadFile("../../testdata/grafana/annotation-request.json")
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(reqRaw))
	dec.DisallowUnknownFields() // a key the sink doesn't produce means drift
	var a grafanaAnnotation
	if err := dec.Decode(&a); err != nil {
		t.Fatalf("request fixture no longer matches sink body type: %v", err)
	}
	if a.Time == 0 || len(a.Tags) == 0 || a.Text == "" {
		t.Fatalf("request fixture missing fields: %+v", a)
	}
	if a.Tags[0] != "wtc" {
		t.Fatalf("tags = %v, want leading wtc", a.Tags)
	}

	respRaw, err := os.ReadFile("../../testdata/grafana/annotation-response.json")
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		ID      int64  `json:"id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respRaw, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == 0 || resp.Message != "Annotation added" {
		t.Fatalf("response fixture = %+v", resp)
	}
}
