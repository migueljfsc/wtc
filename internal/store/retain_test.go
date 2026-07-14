package store

import (
	"context"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

func TestRetain(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

	// age(d) returns a timestamp d days before now.
	age := func(days int) time.Time { return now.Add(-time.Duration(days) * 24 * time.Hour) }

	seed := []*model.Event{
		testEvent("keep:recent-prod", age(10), func(e *model.Event) { e.Env = "prod" }),
		testEvent("del:old-prod", age(200), func(e *model.Event) { e.Env = "prod" }),
		testEvent("keep:recent-unmapped", age(10), func(e *model.Event) { e.Env = "" }),
		testEvent("del:old-unmapped", age(200), func(e *model.Event) { e.Env = "" }),
		testEvent("keep:recent-pr", age(5), func(e *model.Event) { e.Env = "pr-123" }),
		// Ephemeral row older than ephemeralKeep but younger than keep: the
		// ephemeral arm must still prune it (it would survive the normal keep).
		testEvent("del:stale-pr", age(40), func(e *model.Event) {
			e.Env = "pr-124"
			e.Title = "ephemeral pr-124 deploy"
		}),
	}
	for _, ev := range seed {
		if _, _, err := s.Ingest(ctx, ev); err != nil {
			t.Fatalf("Ingest %s: %v", ev.DedupKey, err)
		}
	}

	res, err := s.Retain(ctx, now, 180*24*time.Hour, 30*24*time.Hour, "pr-*")
	if err != nil {
		t.Fatalf("Retain: %v", err)
	}
	if res.DeletedNormal != 2 {
		t.Fatalf("DeletedNormal = %d, want 2 (old-prod + old-unmapped)", res.DeletedNormal)
	}
	if res.DeletedEphemeral != 1 {
		t.Fatalf("DeletedEphemeral = %d, want 1 (stale-pr)", res.DeletedEphemeral)
	}

	events, _, err := s.ListEvents(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	survived := map[string]bool{}
	for _, ev := range events {
		survived[ev.DedupKey] = true
	}
	want := []string{"keep:recent-prod", "keep:recent-unmapped", "keep:recent-pr"}
	if len(survived) != len(want) {
		t.Fatalf("survivors = %v, want %v", keys(survived), want)
	}
	for _, k := range want {
		if !survived[k] {
			t.Fatalf("expected %s to survive; survivors = %v", k, keys(survived))
		}
	}

	// FTS must drop the pruned rows too (events_fts_ad trigger).
	hits, _, err := s.ListEvents(ctx, Filter{Query: "ephemeral"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("FTS still returns a pruned row: %v", hits)
	}
}

// TestRetainNoEphemeralPattern: an empty pattern disables the ephemeral arm;
// every row (pr-* included) falls under the single keep window.
func TestRetainNoEphemeralPattern(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	old := now.Add(-100 * 24 * time.Hour)
	if _, _, err := s.Ingest(ctx, testEvent("pr:old", old, func(e *model.Event) { e.Env = "pr-9" })); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Ingest(ctx, testEvent("prod:new", now, func(e *model.Event) { e.Env = "prod" })); err != nil {
		t.Fatal(err)
	}

	res, err := s.Retain(ctx, now, 90*24*time.Hour, 0, "")
	if err != nil {
		t.Fatalf("Retain: %v", err)
	}
	if res.DeletedNormal != 1 || res.DeletedEphemeral != 0 {
		t.Fatalf("res = %+v, want {DeletedNormal:1 DeletedEphemeral:0}", res)
	}
}

func TestRetainRejectsNonPositiveKeep(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.Retain(context.Background(), time.Now(), 0, 0, "pr-*"); err == nil {
		t.Fatal("Retain with keep=0: want error")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
