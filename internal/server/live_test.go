package server

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/normalize"
)

func TestConfigEndpoint(t *testing.T) {
	st := newTestStore(t)
	rules := []normalize.Rule{{
		Match: normalize.RuleMatch{Source: "github"},
		Set:   normalize.RuleSet{Env: "prod"},
	}}
	srv := New(st, Options{
		Tokens:      []string{testToken},
		Rules:       rules,
		TagPatterns: []string{"^sha-(?P<sha>[0-9a-f]{7,40})$"},
	}, slog.New(slog.DiscardHandler))
	url := newHTTPTest(t, srv)

	if resp, _ := doRequest(t, http.MethodGet, url+"/api/v1/config", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("config without token = %d, want 401", resp.StatusCode)
	}
	resp, body := doRequest(t, http.MethodGet, url+"/api/v1/config", testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("config = %d %s", resp.StatusCode, body)
	}
	var cfg ConfigResponse
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].Set.Env != "prod" || len(cfg.TagPatterns) != 1 {
		t.Fatalf("config body = %+v", cfg)
	}
}

func TestStream(t *testing.T) {
	ts := newTestServer(t)

	// Auth enforced. (doRequest reads the whole body, which is finite for a
	// 401; a 200 stream would block, so only the unauth path uses it.)
	if resp, _ := doRequest(t, http.MethodGet, ts.URL+"/api/v1/stream", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stream without token = %d, want 401", resp.StatusCode)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/stream", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}

	dataCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data:") {
				dataCh <- strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				return
			}
		}
	}()

	ev := []byte(`{"kind":"deploy","env":"prod","service":"api","title":"live deploy","dedup_key":"stream:1"}`)
	if r, b := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, ev); r.StatusCode != http.StatusCreated {
		t.Fatalf("ingest = %d %s", r.StatusCode, b)
	}

	select {
	case data := <-dataCh:
		var got struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal([]byte(data), &got); err != nil {
			t.Fatalf("stream frame not JSON: %q", data)
		}
		if got.Title != "live deploy" {
			t.Fatalf("stream frame title = %q, want 'live deploy'", got.Title)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ingested event did not arrive on the stream")
	}
}
