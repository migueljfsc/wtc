package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// Postgres parity suite. Gated behind WTC_TEST_PG_DSN — sqlite tests
// always run; these re-exercise the dialect-divergent paths (upsert, search,
// doctor, retain, watermarks, overrides, stats, matrix, ledger migration)
// against a real postgres. CI provides a postgres service container; locally:
//
//	docker run -d --name wtc-pg -e POSTGRES_PASSWORD=wtc -e POSTGRES_DB=wtc \
//	  -p 55432:5432 postgres:16-alpine
//	WTC_TEST_PG_DSN='postgres://postgres:wtc@localhost:55432/wtc?sslmode=disable' go test ./internal/store/
//
// Tests share one database and are sequential: the helper truncates all data
// tables at setup, so each test starts clean without re-running migrations.
func openTestPG(t *testing.T) *Store {
	t.Helper()
	pgDSN := os.Getenv("WTC_TEST_PG_DSN")
	if pgDSN == "" {
		t.Skip("WTC_TEST_PG_DSN not set — skipping postgres parity tests")
	}
	s, err := OpenPostgres(pgDSN)
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	if _, err := s.writeDB.ExecContext(context.Background(),
		`TRUNCATE events, github_poll_state, config_overrides`); err != nil {
		_ = s.Close()
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

// TestPGUpsertLifecycle exercises the status-rank upsert — the most
// intricate shared query — on postgres: started→succeeded updates in place,
// a stale started never regresses, and non-empty-wins enrichment holds.
func TestPGUpsertLifecycle(t *testing.T) {
	s := openTestPG(t)
	ctx := context.Background()
	now := time.Now().UTC()

	started := testEvent("pg:build:1", now, func(e *model.Event) {
		e.Status = model.StatusStarted
		e.Env = "" // enriched by the completed event below
	})
	firstID, deduped, err := s.Ingest(ctx, started)
	if err != nil || deduped {
		t.Fatalf("started: id=%s deduped=%v err=%v", firstID, deduped, err)
	}

	dur := int64(90_000)
	completed := testEvent("pg:build:1", now.Add(time.Minute), func(e *model.Event) {
		e.Status = model.StatusSucceeded
		e.Env = "prod"
		e.DurationMS = &dur
	})
	secondID, deduped, err := s.Ingest(ctx, completed)
	if err != nil || !deduped || secondID != firstID {
		t.Fatalf("completed: id=%s deduped=%v err=%v (want dedup onto %s)", secondID, deduped, err, firstID)
	}

	// Stale replay of started must not regress the row.
	if _, _, err := s.Ingest(ctx, started); err != nil {
		t.Fatal(err)
	}

	evs, _, err := s.ListEvents(ctx, Filter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("rows = %d, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Status != model.StatusSucceeded || ev.Env != "prod" {
		t.Errorf("status/env = %s/%s, want succeeded/prod", ev.Status, ev.Env)
	}
	if ev.DurationMS == nil || *ev.DurationMS != dur {
		t.Errorf("duration = %v, want %d", ev.DurationMS, dur)
	}
}

// TestPGListSearchAndCursor covers the ILIKE search branch and cursor paging.
func TestPGListSearchAndCursor(t *testing.T) {
	s := openTestPG(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)

	for i, title := range []string{"Deploy api to prod", "Build worker image", "Deploy web to staging"} {
		ev := testEvent(filterKey("pg:list", i), base.Add(time.Duration(i)*time.Minute),
			func(e *model.Event) { e.Title = title })
		if _, _, err := s.Ingest(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	// Case-insensitive multi-term search (ILIKE branch).
	evs, _, err := s.ListEvents(ctx, Filter{Query: "DEPLOY prod"})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Title != "Deploy api to prod" {
		t.Fatalf("search = %d rows (%+v), want the prod deploy", len(evs), evs)
	}

	// Cursor pagination: page size 2 → 2 + 1.
	page1, cursor, err := s.ListEvents(ctx, Filter{Limit: 2})
	if err != nil || len(page1) != 2 || cursor == "" {
		t.Fatalf("page1 = %d rows cursor=%q err=%v", len(page1), cursor, err)
	}
	page2, cursor2, err := s.ListEvents(ctx, Filter{Limit: 2, Cursor: cursor})
	if err != nil || len(page2) != 1 || cursor2 != "" {
		t.Fatalf("page2 = %d rows cursor=%q err=%v", len(page2), cursor2, err)
	}
}

// TestPGDoctor exercises the postgres variants: pg_database_size, the
// EXTRACT-based clock-skew count, and the churn heuristic's HAVING form.
func TestPGDoctor(t *testing.T) {
	s := openTestPG(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// One skewed event (ts an hour behind ingested_at).
	skewed := testEvent("pg:skew:1", now.Add(-time.Hour))
	if _, _, err := s.Ingest(ctx, skewed); err != nil {
		t.Fatal(err)
	}
	// A churn cluster: 5 distinct keys, same title/kind/status, seconds apart.
	for i := 0; i < 5; i++ {
		ev := testEvent(filterKey("pg:churn", i), now.Add(time.Duration(i)*time.Second),
			func(e *model.Event) { e.Title = "unstable webhook" })
		if _, _, err := s.Ingest(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	rep, err := s.Doctor(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.TotalEvents != 6 {
		t.Errorf("total = %d, want 6", rep.TotalEvents)
	}
	if rep.DBSizeBytes <= 0 {
		t.Errorf("db size = %d, want > 0", rep.DBSizeBytes)
	}
	if rep.ClockSkew24h != 1 {
		t.Errorf("clock skew = %d, want 1", rep.ClockSkew24h)
	}
	found := false
	for _, c := range rep.WebhookChurn {
		if c.Title == "unstable webhook" && c.Rows >= 5 {
			found = true
		}
	}
	if !found {
		t.Errorf("churn not flagged: %+v", rep.WebhookChurn)
	}
}

// TestPGRetain exercises the glob→regex translation and the vacuum skip.
func TestPGRetain(t *testing.T) {
	s := openTestPG(t)
	ctx := context.Background()
	now := time.Now().UTC()

	mk := func(key, env string, age time.Duration) {
		ev := testEvent(key, now.Add(-age), func(e *model.Event) { e.Env = env })
		if _, _, err := s.Ingest(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	mk("pg:old:prod", "prod", 40*24*time.Hour)  // past keep → deleted
	mk("pg:new:prod", "prod", time.Hour)        // recent → kept
	mk("pg:old:pr", "pr-123", 10*24*time.Hour)  // past ephemeral keep → deleted
	mk("pg:new:pr", "pr-124", 2*24*time.Hour)   // inside ephemeral keep → kept
	mk("pg:prlike", "primary", 10*24*time.Hour) // must NOT match pr-* glob → kept (inside keep)

	res, err := s.Retain(ctx, now, 30*24*time.Hour, 7*24*time.Hour, "pr-*")
	if err != nil {
		t.Fatal(err)
	}
	if res.DeletedNormal != 1 || res.DeletedEphemeral != 1 {
		t.Errorf("deleted = %+v, want 1 normal + 1 ephemeral", res)
	}
	evs, _, err := s.ListEvents(ctx, Filter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Errorf("remaining = %d, want 3", len(evs))
	}
	for _, ev := range evs {
		if ev.Env == "pr-123" || ev.DedupKey == "pg:old:prod" {
			t.Errorf("row should have been pruned: %+v", ev)
		}
	}
}

// TestPGWatermarksAndOverrides covers the qualified ON CONFLICT WHERE and the
// config-override upsert.
func TestPGWatermarksAndOverrides(t *testing.T) {
	s := openTestPG(t)
	ctx := context.Background()
	t1 := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)

	if err := s.SetPollWatermark(ctx, "acme/api", "runs", t1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPollWatermark(ctx, "acme/api", "runs", t2); err != nil {
		t.Fatal(err)
	}
	// Monotonic: an older watermark must be ignored.
	if err := s.SetPollWatermark(ctx, "acme/api", "runs", t1); err != nil {
		t.Fatal(err)
	}
	wm, err := s.PollWatermark(ctx, "acme/api", "runs")
	if err != nil {
		t.Fatal(err)
	}
	if !wm.Equal(t2) {
		t.Errorf("watermark = %s, want %s", wm, t2)
	}

	if err := s.SetConfigOverride(ctx, "rules", `[{"match":{}}]`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConfigOverride(ctx, "rules", `[]`); err != nil {
		t.Fatal(err)
	}
	v, ok, err := s.GetConfigOverride(ctx, "rules")
	if err != nil || !ok || v != `[]` {
		t.Errorf("override = %q ok=%v err=%v, want [] true nil", v, ok, err)
	}
}

// TestPGStatsAndMatrix covers the substr bucketing and the matrix env listing.
func TestPGStatsAndMatrix(t *testing.T) {
	s := openTestPG(t)
	ctx := context.Background()
	day := time.Date(2026, 7, 16, 9, 30, 0, 0, time.UTC)

	for i, env := range []string{"prod", "staging"} {
		ev := testEvent(filterKey("pg:deploy", i), day.Add(time.Duration(i)*time.Hour), func(e *model.Event) {
			e.Kind = model.KindDeploy
			e.Status = model.StatusSucceeded
			e.Env = env
			e.Ref = "abc1234"
		})
		if _, _, err := s.Ingest(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := s.ActivityStats(ctx, day.Add(-time.Hour), day.Add(3*time.Hour), "hour")
	if err != nil {
		t.Fatal(err)
	}
	var total int
	for _, b := range stats.Buckets {
		total += b.Total
	}
	if total != 2 {
		t.Errorf("activity total = %d, want 2 (buckets %+v)", total, stats.Buckets)
	}

	m, err := s.Matrix(ctx, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Envs) != 2 || len(m.Services) != 1 {
		t.Errorf("matrix = envs %v services %d, want 2 envs / 1 service", m.Envs, len(m.Services))
	}
}

// TestPGMigrateLedger runs the sqlite→postgres one-shot copy end-to-end and
// asserts idempotent re-run.
func TestPGMigrateLedger(t *testing.T) {
	pgDSN := os.Getenv("WTC_TEST_PG_DSN")
	if pgDSN == "" {
		t.Skip("WTC_TEST_PG_DSN not set")
	}
	ctx := context.Background()

	// Build a small sqlite ledger.
	path := filepath.Join(t.TempDir(), "wtc.db")
	src, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if _, _, err := src.Ingest(ctx, testEvent(filterKey("mig", i), now.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	if err := src.SetPollWatermark(ctx, "acme/api", "runs", now); err != nil {
		t.Fatal(err)
	}
	if err := src.SetConfigOverride(ctx, "rules", `[]`); err != nil {
		t.Fatal(err)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}

	// Clean destination, then migrate.
	pg := openTestPG(t)
	if err := pg.Close(); err != nil { // MigrateLedger opens its own handle
		t.Fatal(err)
	}
	res, err := MigrateLedger(ctx, path, pgDSN)
	if err != nil {
		t.Fatal(err)
	}
	if res.Events != 3 || res.Watermarks != 1 || res.Overrides != 1 {
		t.Fatalf("migrate = %+v, want 3/1/1", res)
	}

	// Idempotent re-run: everything skips.
	res2, err := MigrateLedger(ctx, path, pgDSN)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Events != 0 || res2.EventsSkipped != 3 {
		t.Fatalf("re-run = %+v, want 0 copied / 3 skipped", res2)
	}

	// The migrated ledger answers queries.
	dst, err := OpenPostgres(pgDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dst.Close() }()
	evs, _, err := dst.ListEvents(ctx, Filter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Errorf("migrated events = %d, want 3", len(evs))
	}
	wm, err := dst.PollWatermark(ctx, "acme/api", "runs")
	if err != nil || wm.IsZero() {
		t.Errorf("migrated watermark = %s err=%v", wm, err)
	}
}

// filterKey builds distinct dedup keys in loops.
func filterKey(prefix string, i int) string {
	return prefix + ":" + string(rune('a'+i))
}

// TestPGNotifyFuncTransitions is the postgres parity check for the notify hook:
// the multi-column RETURNING (merged row) and transition detection must
// behave identically to sqlite.
func TestPGNotifyFuncTransitions(t *testing.T) {
	s := openTestPG(t)
	ctx := context.Background()
	base := time.Now().UTC()

	var events []model.Event
	var transitions []bool
	s.SetNotifyFunc(func(ev model.Event, transitioned bool) {
		events = append(events, ev)
		transitions = append(transitions, transitioned)
	})

	key := fmt.Sprintf("pg:notify:%d", base.UnixNano())
	started := testEvent(key, base, func(e *model.Event) { e.Status = model.StatusStarted })
	if _, _, err := s.Ingest(ctx, started); err != nil {
		t.Fatalf("started: %v", err)
	}
	failed := testEvent(key, base.Add(time.Minute), func(e *model.Event) {
		e.Status = model.StatusFailed
		e.Env = "" // merged row must carry env from the started row
	})
	if _, _, err := s.Ingest(ctx, failed); err != nil {
		t.Fatalf("failed: %v", err)
	}
	redelivered := testEvent(key, base.Add(time.Minute), func(e *model.Event) { e.Status = model.StatusFailed })
	if _, _, err := s.Ingest(ctx, redelivered); err != nil {
		t.Fatalf("redelivery: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("notify calls = %d, want 2 (new row + transition, no redelivery)", len(events))
	}
	if transitions[0] || !transitions[1] {
		t.Fatalf("transitions = %v, want [false true]", transitions)
	}
	if events[1].Status != model.StatusFailed || events[1].Env != "dev" || events[1].ID != started.ID {
		t.Fatalf("merged transition event = %+v", events[1])
	}
}
