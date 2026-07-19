package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/notify"
	"github.com/migueljfsc/wtc/internal/store"
)

// TestNotifyE2E replays a deploy lifecycle through the full HTTP surface with
// the notification dispatcher wired the way serve.go wires it, and asserts the accept
// criteria: a `status: failed` subscription fires when the row UPSERTS to
// failed (not on the started row), exactly once even when the failed payload
// is redelivered.
func TestNotifyE2E(t *testing.T) {
	slackTexts := make(chan string, 10)
	slackSrv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var msg struct {
			Text string `json:"text"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &msg); err != nil {
			t.Errorf("slack body: %v", err)
		}
		slackTexts <- msg.Text
	}))
	defer slackSrv.Close()

	subs, err := notify.Compile([]notify.Subscription{{
		Name:  "failures",
		Match: notify.Match{Env: "prod", Status: "failed"},
		Sink:  notify.Sink{Type: "slack", URL: slackSrv.URL},
	}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	d := notify.NewDispatcher(subs, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	st.SetNotifyFunc(d.Enqueue) // same wiring as serve.go
	srv := New(st, Options{Tokens: []string{testToken}}, slog.New(slog.DiscardHandler))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})

	post := func(body string, wantStatus int) {
		t.Helper()
		resp, data := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, []byte(body))
		if resp.StatusCode != wantStatus {
			t.Fatalf("ingest = %d %s, want %d", resp.StatusCode, data, wantStatus)
		}
	}

	// started: matches env but not status — must not notify.
	post(`{"kind":"deploy","env":"prod","service":"api","status":"started",
	      "title":"deploy api v2","dedup_key":"e2e:notify:1"}`, http.StatusCreated)

	// failed: upsert transitions the SAME row started→failed — must notify once.
	post(`{"kind":"deploy","env":"prod","service":"api","status":"failed",
	      "title":"deploy api v2 failed","dedup_key":"e2e:notify:1"}`, http.StatusOK)

	select {
	case text := <-slackTexts:
		if !strings.Contains(text, "failed") || !strings.Contains(text, "[prod]") {
			t.Fatalf("slack text = %q", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("failed-status notification never arrived")
	}

	// Redelivery of the failed payload: rank-suppressed dedup, no re-notify.
	post(`{"kind":"deploy","env":"prod","service":"api","status":"failed",
	      "title":"deploy api v2 failed","dedup_key":"e2e:notify:1"}`, http.StatusOK)

	select {
	case text := <-slackTexts:
		t.Fatalf("redelivery re-notified: %q", text)
	case <-time.After(500 * time.Millisecond):
	}
}
