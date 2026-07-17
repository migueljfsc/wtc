package server

import (
	"bytes"
	"context"
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

	"github.com/migueljfsc/wtc/internal/ingest/mapping"
	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/store"
)

// webhookTestServer builds a server wired with the given mapping webhooks and
// returns the httptest server plus the store (for post-ingest assertions).
func webhookTestServer(t *testing.T, webhooks []mapping.Webhook) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	mappers, err := mapping.Compile(webhooks)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for name := range mappers {
		model.RegisterSource(model.Source(name))
	}
	srv := New(st, Options{Tokens: []string{testToken}, Mappers: mappers}, slog.New(slog.DiscardHandler))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		_ = st.Close()
	})
	return ts, st
}

func jenkinsWebhook() mapping.Webhook {
	return mapping.Webhook{Name: "jenkins", Preset: "jenkins", Auth: mapping.Auth{Token: "jenkins-secret", Header: "X-WTC-Token"}}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../testdata/webhook", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWebhookUnknownName(t *testing.T) {
	ts, _ := webhookTestServer(t, []mapping.Webhook{jenkinsWebhook()})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/ingest/webhook/nope", nil)
	req.Header.Set("X-WTC-Token", "jenkins-secret")
	resp, _ := http.DefaultClient.Do(req)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown webhook = %d, want 404", resp.StatusCode)
	}
}

func TestWebhookStaticTokenAuth(t *testing.T) {
	ts, _ := webhookTestServer(t, []mapping.Webhook{jenkinsWebhook()})
	body := readFixture(t, "jenkins-started.json")
	cases := []struct {
		name, token string
		want        int
	}{
		{"valid", "jenkins-secret", http.StatusCreated},
		{"wrong", "nope", http.StatusUnauthorized},
		{"missing", "", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/ingest/webhook/jenkins", bytes.NewReader(body))
			if c.token != "" {
				req.Header.Set("X-WTC-Token", c.token)
			}
			resp, _ := http.DefaultClient.Do(req)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != c.want {
				t.Fatalf("auth %s = %d, want %d", c.name, resp.StatusCode, c.want)
			}
		})
	}
}

func TestWebhookHMACAuth(t *testing.T) {
	wh := mapping.Webhook{Name: "signed", Preset: "jenkins",
		Auth: mapping.Auth{HMAC: &mapping.HMACAuth{Secret: "topsecret", Header: "X-Sig", Algo: "sha256", Prefix: "sha256="}}}
	ts, _ := webhookTestServer(t, []mapping.Webhook{wh})
	body := readFixture(t, "jenkins-completed.json")

	mac := hmac.New(sha256.New, []byte("topsecret"))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// Valid signature.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/ingest/webhook/signed", bytes.NewReader(body))
	req.Header.Set("X-Sig", sig)
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("valid hmac = %d, want 201", resp.StatusCode)
	}

	// Tampered signature.
	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/ingest/webhook/signed", bytes.NewReader(body))
	req2.Header.Set("X-Sig", "sha256=deadbeef")
	resp2, _ := http.DefaultClient.Do(req2)
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad hmac = %d, want 401", resp2.StatusCode)
	}
}

// TestWebhookLifecycleUpsert drives a build STARTED then COMPLETED through the
// endpoint and asserts one row whose status advanced to succeeded (trap #5).
func TestWebhookLifecycleUpsert(t *testing.T) {
	ts, st := webhookTestServer(t, []mapping.Webhook{jenkinsWebhook()})

	post := func(fixture string) int {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/ingest/webhook/jenkins", bytes.NewReader(readFixture(t, fixture)))
		req.Header.Set("X-WTC-Token", "jenkins-secret")
		resp, _ := http.DefaultClient.Do(req)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if code := post("jenkins-started.json"); code != http.StatusCreated {
		t.Fatalf("started = %d, want 201", code)
	}
	if code := post("jenkins-completed.json"); code != http.StatusOK {
		t.Fatalf("completed = %d, want 200 (dedup upsert)", code)
	}

	evs, _, err := st.ListEvents(context.Background(), store.Filter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("got %d rows, want 1 (one build)", len(evs))
	}
	if evs[0].Status != model.StatusSucceeded {
		t.Errorf("status = %s, want succeeded", evs[0].Status)
	}
	if evs[0].Source != "jenkins" {
		t.Errorf("source = %s, want jenkins", evs[0].Source)
	}
}

// TestWebhookMappingErrorInDoctor asserts a template failure returns 422 and is
// surfaced in the doctor report (never a silent drop).
func TestWebhookMappingErrorInDoctor(t *testing.T) {
	// dedup_key references a field that isn't in the body → renders empty.
	wh := mapping.Webhook{Name: "broken", Auth: mapping.Auth{Token: "s"},
		DedupKey: "{{ .never_present }}", Mapping: mapping.FieldTemplates{Kind: "build", Title: "t"}}
	ts, _ := webhookTestServer(t, []mapping.Webhook{wh})

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/ingest/webhook/broken", bytes.NewReader([]byte(`{"a":1}`)))
	req.Header.Set("X-WTC-Token", "s")
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("mapping error = %d, want 422", resp.StatusCode)
	}

	// The in-memory tracker is merged into the report by the doctor handler.
	resp2, body := doRequest(t, http.MethodGet, ts.URL+"/api/doctor", testToken, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("doctor = %d", resp2.StatusCode)
	}
	var doc store.DoctorReport
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.WebhookMappingErrors) != 1 || doc.WebhookMappingErrors[0].Source != "broken" {
		t.Fatalf("doctor mapping errors = %+v, want one for 'broken'", doc.WebhookMappingErrors)
	}
	if doc.WebhookMappingErrors[0].Count != 1 {
		t.Errorf("error count = %d, want 1", doc.WebhookMappingErrors[0].Count)
	}
}
