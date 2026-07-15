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

func TestConfigEdit(t *testing.T) {
	st := newTestStore(t)
	fileRules := []normalize.Rule{{
		Match: normalize.RuleMatch{Source: "flux"},
		Set:   normalize.RuleSet{Env: "file-env"},
	}}
	newSrv := func() string {
		eng, _ := normalize.NewEngine(fileRules)
		srv := New(st, Options{
			Tokens:      []string{testToken},
			Engine:      normalize.NewEngineHolder(eng),
			Rules:       fileRules,
			TagPatterns: normalize.DefaultTagPatterns,
		}, slog.New(slog.DiscardHandler))
		return newHTTPTest(t, srv)
	}
	url := newSrv()

	get := func(u string) ConfigResponse {
		_, body := doRequest(t, http.MethodGet, u+"/api/v1/config", testToken, nil)
		var cfg ConfigResponse
		if err := json.Unmarshal(body, &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg
	}

	if cfg := get(url); cfg.RulesOverridden {
		t.Fatal("baseline must not be marked overridden")
	}

	// Edit: valid rules → hot-reloaded + persisted.
	put := []byte(`{"rules":[{"match":{"source":"flux"},"set":{"env":"edited"}}]}`)
	if resp, body := doRequest(t, http.MethodPut, url+"/api/v1/config/rules", testToken, put); resp.StatusCode != http.StatusOK {
		t.Fatalf("put rules = %d %s", resp.StatusCode, body)
	}
	if cfg := get(url); !cfg.RulesOverridden || len(cfg.Rules) != 1 || cfg.Rules[0].Set.Env != "edited" {
		t.Fatalf("after edit config = %+v", cfg)
	}

	// A rule that won't compile (unclosed template) is rejected; nothing changes.
	bad := []byte(`{"rules":[{"match":{"source":"flux"},"set":{"env":"{{ .Repo"}}]}`)
	if resp, _ := doRequest(t, http.MethodPut, url+"/api/v1/config/rules", testToken, bad); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid rule must be 400")
	}
	if cfg := get(url); cfg.Rules[0].Set.Env != "edited" {
		t.Fatal("rejected edit must not change the live config")
	}

	// Persisted: a fresh server over the same store loads the override.
	if cfg := get(newSrv()); !cfg.RulesOverridden || cfg.Rules[0].Set.Env != "edited" {
		t.Fatalf("override not persisted/reloaded: %+v", cfg)
	}

	// Reset: back to the YAML baseline.
	if resp, _ := doRequest(t, http.MethodDelete, url+"/api/v1/config/rules", testToken, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("reset failed")
	}
	if cfg := get(url); cfg.RulesOverridden || cfg.Rules[0].Set.Env != "file-env" {
		t.Fatalf("after reset config = %+v", cfg)
	}

	// Auth enforced on the mutating routes.
	if resp, _ := doRequest(t, http.MethodPut, url+"/api/v1/config/rules", "", put); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("put without token = %d, want 401", resp.StatusCode)
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
