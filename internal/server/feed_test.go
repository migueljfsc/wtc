package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestFeed(t *testing.T) {
	ts := newTestServer(t)
	for _, body := range []string{
		`{"kind":"deploy","env":"prod","service":"api","status":"succeeded",
		  "title":"deploy api v3","url":"https://ci.example.com/1","dedup_key":"feed:1"}`,
		`{"kind":"merge","env":"staging","service":"web","status":"succeeded",
		  "title":"merge web PR 7","dedup_key":"feed:2"}`,
	} {
		resp, data := doRequest(t, http.MethodPost, ts.URL+"/ingest/generic", testToken, []byte(body))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed ingest = %d %s", resp.StatusCode, data)
		}
	}

	t.Run("auth required", func(t *testing.T) {
		resp, _ := doRequest(t, http.MethodGet, ts.URL+"/feed", "", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauthenticated feed = %d, want 401", resp.StatusCode)
		}
		resp, _ = doRequest(t, http.MethodGet, ts.URL+"/feed?token=nope", "", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong token = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("query token", func(t *testing.T) {
		resp, body := doRequest(t, http.MethodGet, ts.URL+"/feed?token="+testToken, "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("feed = %d %s, want 200", resp.StatusCode, body)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/atom+xml") {
			t.Fatalf("content-type = %q", ct)
		}
		text := string(body)
		for _, want := range []string{
			`xmlns="http://www.w3.org/2005/Atom"`,
			"[prod] deploy api succeeded — deploy api v3",
			`href="https://ci.example.com/1"`,
			"urn:wtc:", ":succeeded",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("feed missing %q:\n%s", want, text)
			}
		}
	})

	t.Run("bearer header and env filter", func(t *testing.T) {
		resp, body := doRequest(t, http.MethodGet, ts.URL+"/feed?env=staging", testToken, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("feed = %d %s", resp.StatusCode, body)
		}
		text := string(body)
		if !strings.Contains(text, "merge web PR 7") || strings.Contains(text, "deploy api v3") {
			t.Fatalf("env filter not applied:\n%s", text)
		}
	})

	t.Run("bad limit", func(t *testing.T) {
		resp, _ := doRequest(t, http.MethodGet, ts.URL+"/feed?limit=zero", testToken, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("bad limit = %d, want 400", resp.StatusCode)
		}
	})
}
