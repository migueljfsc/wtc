package store

import (
	"context"
	"testing"
	"time"
)

func TestBroadcast(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	sub := s.Subscribe()
	defer s.Unsubscribe(sub)

	// A new event is broadcast.
	if _, _, err := s.Ingest(ctx, testEvent("b:1", now)); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	select {
	case got := <-sub:
		if got.DedupKey != "b:1" {
			t.Fatalf("broadcast event = %q, want b:1", got.DedupKey)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a broadcast for the new event")
	}

	// A re-ingested duplicate (deduped) must NOT broadcast — no live flooding
	// from poller sweeps / redeliveries.
	if _, deduped, err := s.Ingest(ctx, testEvent("b:1", now)); err != nil || !deduped {
		t.Fatalf("duplicate ingest: deduped=%v err=%v", deduped, err)
	}
	select {
	case got := <-sub:
		t.Fatalf("duplicate must not broadcast, got %q", got.DedupKey)
	case <-time.After(200 * time.Millisecond):
	}
}
