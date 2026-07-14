package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSlackPostsText(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	if err := Slack(context.Background(), srv.URL, "*hello* world"); err != nil {
		t.Fatal(err)
	}
	if got["text"] != "*hello* world" {
		t.Errorf("posted text = %q", got["text"])
	}
}

func TestSlackReportsRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid_payload", http.StatusBadRequest)
	}))
	defer srv.Close()

	err := Slack(context.Background(), srv.URL, "x")
	if err == nil {
		t.Fatal("want error on non-200 from slack")
	}
}
