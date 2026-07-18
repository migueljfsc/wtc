package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func testEvent(dedupKey string, ts time.Time, mutate ...func(*model.Event)) *model.Event {
	ev := &model.Event{
		ID:         model.NewID(),
		TS:         ts,
		IngestedAt: time.Now().UTC(),
		Source:     model.SourceGeneric,
		Kind:       model.KindManual,
		Status:     model.StatusUnknown,
		Env:        "dev",
		Service:    "api",
		Title:      "test event",
		DedupKey:   dedupKey,
	}
	for _, m := range mutate {
		m(ev)
	}
	return ev
}

func TestIngestAndList(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	ev := testEvent("t:1", now)
	id, deduped, err := s.Ingest(ctx, ev)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if deduped {
		t.Fatal("first ingest must not be deduped")
	}
	if id != ev.ID {
		t.Fatalf("id = %s, want %s", id, ev.ID)
	}

	events, cursor, err := s.ListEvents(ctx, Filter{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 || cursor != "" {
		t.Fatalf("got %d events, cursor %q; want 1 event, empty cursor", len(events), cursor)
	}
	got := events[0]
	if got.Title != "test event" || got.Env != "dev" || got.Service != "api" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !got.TS.Equal(now.Truncate(time.Millisecond)) {
		t.Fatalf("ts = %v, want %v (ms precision)", got.TS, now.Truncate(time.Millisecond))
	}
}

func TestIngestValidates(t *testing.T) {
	s := openTestStore(t)
	ev := testEvent("t:bad", time.Now(), func(e *model.Event) { e.Kind = "nope" })
	if _, _, err := s.Ingest(context.Background(), ev); err == nil {
		t.Fatal("Ingest with invalid kind: want error")
	}
}

func TestDedupUpsertLifecycle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC()

	started := testEvent("gh:run:org/app:1:1", base, func(e *model.Event) {
		e.Status = model.StatusStarted
		e.Title = "build #1 started"
	})
	firstID, deduped, err := s.Ingest(ctx, started)
	if err != nil || deduped {
		t.Fatalf("started: err=%v deduped=%v", err, deduped)
	}

	// Same logical change completes: must update the SAME row. The
	// completion omits env/payload — merge semantics must keep the earlier
	// values, and its non-empty ref must enrich the row.
	dur := int64(90_000)
	completed := testEvent("gh:run:org/app:1:1", base.Add(90*time.Second), func(e *model.Event) {
		e.Status = model.StatusSucceeded
		e.Title = "build #1 succeeded"
		e.DurationMS = &dur
		e.Env = "" // later event lacks env: must not blank the stored one
		e.Ref = "abc1234"
	})
	secondID, deduped, err := s.Ingest(ctx, completed)
	if err != nil {
		t.Fatalf("completed: %v", err)
	}
	if !deduped || secondID != firstID {
		t.Fatalf("completed: deduped=%v id=%s, want dedup onto %s", deduped, secondID, firstID)
	}

	// Late-arriving "started" must NOT regress the terminal row.
	late := testEvent("gh:run:org/app:1:1", base, func(e *model.Event) {
		e.Status = model.StatusStarted
		e.Title = "late replay"
	})
	if _, _, err := s.Ingest(ctx, late); err != nil {
		t.Fatalf("late started: %v", err)
	}

	events, _, err := s.ListEvents(ctx, Filter{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d rows, want exactly 1 (upsert, not insert-per-transition)", len(events))
	}
	got := events[0]
	if got.Status != model.StatusSucceeded || got.Title != "build #1 succeeded" {
		t.Fatalf("row regressed: status=%s title=%q", got.Status, got.Title)
	}
	if got.DurationMS == nil || *got.DurationMS != dur {
		t.Fatalf("duration_ms = %v, want %d", got.DurationMS, dur)
	}
	if got.Env != "dev" {
		t.Fatalf("env = %q, want %q — completion without env must not blank it", got.Env, "dev")
	}
	if got.Ref != "abc1234" {
		t.Fatalf("ref = %q, want abc1234 — completion's ref must enrich the row", got.Ref)
	}
}

func TestUpsertMergePreservesPayload(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC()

	started := testEvent("gh:run:org/app:2:1", base, func(e *model.Event) {
		e.Status = model.StatusStarted
		e.Payload = `{"artifacts":["reg/app:sha-abc1234"]}`
	})
	if _, _, err := s.Ingest(ctx, started); err != nil {
		t.Fatal(err)
	}

	// Completion carries no payload: the artifact list must survive (the
	// tag↔sha join for `wtc where` depends on it — CLAUDE.md trap #8).
	completed := testEvent("gh:run:org/app:2:1", base.Add(time.Minute), func(e *model.Event) {
		e.Status = model.StatusSucceeded
	})
	if _, _, err := s.Ingest(ctx, completed); err != nil {
		t.Fatal(err)
	}

	events, _, err := s.ListEvents(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d rows, want 1", len(events))
	}
	if events[0].Payload != `{"artifacts":["reg/app:sha-abc1234"]}` {
		t.Fatalf("payload = %q — completion without payload must not destroy it", events[0].Payload)
	}
	if events[0].Status != model.StatusSucceeded {
		t.Fatalf("status = %s, want succeeded", events[0].Status)
	}
}

func TestUpsertStrictRankNoEqualRankFlip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC()

	succeeded := testEvent("gh:run:org/app:3:1", base.Add(time.Minute), func(e *model.Event) {
		e.Status = model.StatusSucceeded
		e.Title = "deploy succeeded"
	})
	if _, _, err := s.Ingest(ctx, succeeded); err != nil {
		t.Fatal(err)
	}

	// A stale equal-rank terminal event redelivered later must NOT flip the
	// row or move ts backward (SPEC §1: only when the incoming status
	// OUTRANKS the stored one).
	staleFailed := testEvent("gh:run:org/app:3:1", base, func(e *model.Event) {
		e.Status = model.StatusFailed
		e.Title = "deploy failed"
	})
	_, deduped, err := s.Ingest(ctx, staleFailed)
	if err != nil {
		t.Fatal(err)
	}
	if !deduped {
		t.Fatal("stale terminal event must dedup onto the existing row")
	}

	events, _, err := s.ListEvents(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d rows, want 1", len(events))
	}
	if events[0].Status != model.StatusSucceeded || events[0].Title != "deploy succeeded" {
		t.Fatalf("equal-rank replay flipped the row: status=%s title=%q", events[0].Status, events[0].Title)
	}
}

func TestIngestAfterCloseAndConcurrentClose(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Hammer Ingest from several goroutines while Close runs: must not
	// panic (send on closed channel); late calls get ErrStoreClosed.
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := range 50 {
				ev := testEvent(fmt.Sprintf("race:%d:%d", n, j), time.Now().UTC())
				if _, _, err := s.Ingest(ctx, ev); err != nil && !errors.Is(err, ErrStoreClosed) {
					t.Errorf("Ingest: unexpected error %v", err)
					return
				}
			}
		}(i)
	}
	time.Sleep(2 * time.Millisecond) // let some ingests land first
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wg.Wait()

	if _, _, err := s.Ingest(ctx, testEvent("race:after", time.Now().UTC())); !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("Ingest after Close = %v, want ErrStoreClosed", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close must be idempotent, got %v", err)
	}
}

func TestListFilters(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	seed := []*model.Event{
		testEvent("f:1", base, func(e *model.Event) { e.Env = "prod"; e.Kind = model.KindDeploy; e.Service = "api" }),
		testEvent("f:2", base.Add(time.Hour), func(e *model.Event) {
			e.Env = "dev"
			e.Kind = model.KindBuild
			e.Service = "api"
			e.Source = model.SourceGitHub
		}),
		testEvent("f:3", base.Add(2*time.Hour), func(e *model.Event) {
			e.Env = "prod"
			e.Kind = model.KindDeploy
			e.Service = "web"
			e.Status = model.StatusFailed
		}),
	}
	for _, ev := range seed {
		if _, _, err := s.Ingest(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name   string
		filter Filter
		want   []string // dedup keys, newest first
	}{
		{"all", Filter{}, []string{"f:3", "f:2", "f:1"}},
		{"env", Filter{Envs: []string{"prod"}}, []string{"f:3", "f:1"}},
		{"kind", Filter{Kinds: []string{"build"}}, []string{"f:2"}},
		{"service", Filter{Services: []string{"api"}}, []string{"f:2", "f:1"}},
		{"status", Filter{Statuses: []string{"failed"}}, []string{"f:3"}},
		{"env OR-set", Filter{Envs: []string{"prod", "dev"}}, []string{"f:3", "f:2", "f:1"}},
		{"since", Filter{Since: base.Add(30 * time.Minute)}, []string{"f:3", "f:2"}},
		{"until", Filter{Until: base.Add(30 * time.Minute)}, []string{"f:1"}},
		{"combo", Filter{Envs: []string{"prod"}, Since: base.Add(time.Hour)}, []string{"f:3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, _, err := s.ListEvents(ctx, tt.filter)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, ev := range events {
				got = append(got, ev.DedupKey)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestCursorPagination(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	const total = 7
	for i := range total {
		ev := testEvent(
			"p:"+string(rune('a'+i)),
			base.Add(time.Duration(i)*time.Minute),
		)
		if _, _, err := s.Ingest(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	var seen []string
	cursor := ""
	pages := 0
	for {
		events, next, err := s.ListEvents(ctx, Filter{Limit: 3, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range events {
			seen = append(seen, ev.DedupKey)
		}
		pages++
		if next == "" {
			break
		}
		cursor = next
		if pages > 5 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != total {
		t.Fatalf("paginated %d events, want %d (no dupes/gaps): %v", len(seen), total, seen)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i-1] <= seen[i] { // keys p:a..p:g were inserted oldest-first
			t.Fatalf("order violated at %d: %v", i, seen)
		}
	}
	if pages != 3 {
		t.Fatalf("pages = %d, want 3 (3+3+1)", pages)
	}
}

func TestFullTextSearch(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)

	seed := []*model.Event{
		testEvent("q:1", base, func(e *model.Event) {
			e.Title = "rotated payments database credentials"
			e.Service = "payments-api"
		}),
		testEvent("q:2", base.Add(time.Minute), func(e *model.Event) {
			e.Title = "deploy web frontend"
			e.Service = "web"
			e.Actor = "alice"
		}),
		testEvent("q:3", base.Add(2*time.Minute), func(e *model.Event) {
			e.Title = "bump image"
			e.Artifact = "reg/payments-api:sha-abc1234"
		}),
	}
	for _, ev := range seed {
		if _, _, err := s.Ingest(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		q    string
		want []string
	}{
		{"payments", []string{"q:3", "q:1"}}, // title + artifact, prefix-tokenized
		{"alice", []string{"q:2"}},
		{"credentials", []string{"q:1"}},
		{"rot", []string{"q:1"}}, // prefix match
		{"nosuchterm", nil},
		{`weird"quote`, nil}, // FTS metachars must not error
	}
	for _, tt := range tests {
		t.Run(tt.q, func(t *testing.T) {
			events, _, err := s.ListEvents(ctx, Filter{Query: tt.q})
			if err != nil {
				t.Fatalf("ListEvents(q=%q): %v", tt.q, err)
			}
			var got []string
			for _, ev := range events {
				got = append(got, ev.DedupKey)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}

	// Upsert keeps the index in sync (UPDATE trigger).
	upd := testEvent("q:1", base.Add(3*time.Minute), func(e *model.Event) {
		e.Status = model.StatusSucceeded
		e.Title = "renamed to billing creds rotation"
	})
	if _, _, err := s.Ingest(ctx, upd); err != nil {
		t.Fatal(err)
	}
	events, _, _ := s.ListEvents(ctx, Filter{Query: "billing"})
	if len(events) != 1 || events[0].DedupKey != "q:1" {
		t.Fatalf("post-update search failed: %v", events)
	}
	events, _, _ = s.ListEvents(ctx, Filter{Query: "credentials"})
	if len(events) != 0 {
		t.Fatalf("stale index entry survived update: %v", events)
	}
}

func TestMigrationsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wtc.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s1.Ingest(context.Background(), testEvent("m:1", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	// Re-open: migrations must not re-apply, data must survive.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer func() { _ = s2.Close() }()
	events, _, err := s2.ListEvents(context.Background(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events after re-open, want 1", len(events))
	}
}
