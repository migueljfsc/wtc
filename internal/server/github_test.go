package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/migueljfsc/wtc/internal/store"
)

const testWebhookSecret = "hook-secret"

func newGitHubTestServer(t *testing.T, captureDir string) *httptest.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st, Options{
		Tokens:              []string{testToken},
		GitHubWebhookSecret: testWebhookSecret,
		CaptureDir:          captureDir,
	}, slog.New(slog.DiscardHandler))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return ts
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func githubPost(t *testing.T, url string, body []byte, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp
}

func TestGitHubHMAC(t *testing.T) {
	ts := newGitHubTestServer(t, "")
	body := []byte(`{"action":"completed"}`)
	url := ts.URL + "/ingest/github"

	tests := []struct {
		name string
		sig  string
		want int
	}{
		{"valid signature", sign(testWebhookSecret, body), http.StatusAccepted},
		{"wrong secret", sign("other-secret", body), http.StatusUnauthorized},
		{"missing header", "", http.StatusUnauthorized},
		{"malformed header", "sha256=zznothex", http.StatusUnauthorized},
		{"wrong scheme", "sha1=" + strings.Repeat("a", 40), http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{"X-GitHub-Event": "workflow_run", "X-GitHub-Delivery": "d-1"}
			if tt.sig != "" {
				headers["X-Hub-Signature-256"] = tt.sig
			}
			resp := githubPost(t, url, body, headers)
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}

	// Signature valid for a DIFFERENT body must fail (body binding).
	resp := githubPost(t, url, []byte(`{"tampered":true}`), map[string]string{
		"X-Hub-Signature-256": sign(testWebhookSecret, body),
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tampered body accepted: %d", resp.StatusCode)
	}
}

func TestGitHubWebhookNotConfiguredFailsClosed(t *testing.T) {
	// Server without a webhook secret must reject even signed deliveries.
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	srv := httptest.NewServer(New(st, Options{Tokens: []string{testToken}}, slog.New(slog.DiscardHandler)).Handler())
	defer srv.Close()

	body := []byte(`{}`)
	resp := githubPost(t, srv.URL+"/ingest/github", body, map[string]string{
		"X-Hub-Signature-256": sign(testWebhookSecret, body),
	})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured webhook endpoint = %d, want 503", resp.StatusCode)
	}
}

func TestGitHubCapture(t *testing.T) {
	dir := t.TempDir()
	ts := newGitHubTestServer(t, dir)
	body := []byte(`{"action":"completed","workflow_run":{"id":1}}`)

	resp := githubPost(t, ts.URL+"/ingest/github", body, map[string]string{
		"X-Hub-Signature-256": sign(testWebhookSecret, body),
		"X-GitHub-Event":      "workflow_run",
		"X-GitHub-Delivery":   "abc-123",
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "github", "*workflow_run-abc-123.json"))
	if err != nil || len(matches) != 1 {
		entries, _ := os.ReadDir(filepath.Join(dir, "github"))
		t.Fatalf("captured files = %v (err %v), dir contents: %v", matches, err, entries)
	}
	captured, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(captured) != string(body) {
		t.Fatalf("captured body mismatch: %s", captured)
	}
	// Headers sidecar exists.
	headerFiles, _ := filepath.Glob(filepath.Join(dir, "github", "*.headers"))
	if len(headerFiles) != 1 {
		t.Fatalf("expected 1 headers sidecar, got %v", headerFiles)
	}
}
