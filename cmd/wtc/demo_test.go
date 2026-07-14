package main

import (
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

func TestBuildDemoEventsValid(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	reqs, exampleSHA := buildDemoEvents(now, 7, "testrun")

	if len(reqs) == 0 {
		t.Fatal("no events generated")
	}
	if len(exampleSHA) < 7 {
		t.Fatalf("exampleSHA %q too short", exampleSHA)
	}

	seenKeys := map[string]bool{}
	// Latest succeeded deploy ts per (service, env) — to prove staging/prod drift.
	latest := map[string]time.Time{}

	for _, r := range reqs {
		// Every request must pass the same validation /ingest/generic applies:
		// this rejects a disallowed source, reserved dedup prefix, or bad enum.
		ev, err := r.ToEvent(now)
		if err != nil {
			t.Fatalf("ToEvent(%q): %v", r.DedupKey, err)
		}
		if seenKeys[r.DedupKey] {
			t.Fatalf("duplicate dedup_key %q — repeat runs would collide", r.DedupKey)
		}
		seenKeys[r.DedupKey] = true

		if ev.TS.After(now) {
			t.Fatalf("event %q is in the future: %s", r.DedupKey, ev.TS)
		}
		if ev.Kind == model.KindDeploy && ev.Status == model.StatusSucceeded {
			k := ev.Service + "/" + ev.Env
			if ev.TS.After(latest[k]) {
				latest[k] = ev.TS
			}
		}
	}

	// The seed must produce real staging↔prod drift for at least one service,
	// so `wtc diff staging prod` is non-trivial in the demo.
	drift := false
	for _, svc := range demoServices {
		s, okS := latest[svc+"/staging"]
		p, okP := latest[svc+"/prod"]
		if okS && (!okP || s.After(p)) {
			drift = true
			break
		}
	}
	if !drift {
		t.Fatal("expected staging to lead prod for some service (diff would be empty)")
	}
}

func TestBuildDemoEventsRunNamespacing(t *testing.T) {
	now := time.Now().UTC()
	a, _ := buildDemoEvents(now, 3, "run-a")
	b, _ := buildDemoEvents(now, 3, "run-b")

	keys := map[string]bool{}
	for _, r := range a {
		keys[r.DedupKey] = true
	}
	for _, r := range b {
		if keys[r.DedupKey] {
			t.Fatalf("dedup_key %q collides across runs — repeat demo runs would overwrite", r.DedupKey)
		}
		if !strings.Contains(r.DedupKey, "run-b") {
			t.Fatalf("dedup_key %q missing run nonce", r.DedupKey)
		}
	}
}

func TestBuildDemoEventsSingleDay(t *testing.T) {
	// days==1: no release is old enough to reach prod; must not panic and must
	// still return a usable example sha.
	reqs, sha := buildDemoEvents(time.Now().UTC(), 1, "one")
	if len(reqs) == 0 || len(sha) < 7 {
		t.Fatalf("days=1 produced %d events, sha %q", len(reqs), sha)
	}
}
