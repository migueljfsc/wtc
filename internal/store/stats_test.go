package store

import (
	"context"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

func kind(k model.Kind) func(*model.Event)     { return func(e *model.Event) { e.Kind = k } }
func status(s model.Status) func(*model.Event) { return func(e *model.Event) { e.Status = s } }
func env(v string) func(*model.Event)          { return func(e *model.Event) { e.Env = v } }
func service(v string) func(*model.Event)      { return func(e *model.Event) { e.Service = v } }

func TestActivityStats(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// day0: 2 events (1 succeeded, 1 failed); day1: none; day2: 3 (2 succeeded).
	seed := []struct {
		key string
		ts  time.Time
		st  model.Status
	}{
		{"a:1", base.Add(2 * time.Hour), model.StatusSucceeded},
		{"a:2", base.Add(5 * time.Hour), model.StatusFailed},
		{"a:3", base.Add(48*time.Hour + 1*time.Hour), model.StatusSucceeded},
		{"a:4", base.Add(48*time.Hour + 2*time.Hour), model.StatusSucceeded},
		{"a:5", base.Add(48*time.Hour + 3*time.Hour), model.StatusUnknown},
	}
	for _, e := range seed {
		if _, _, err := s.Ingest(ctx, testEvent(e.key, e.ts, status(e.st))); err != nil {
			t.Fatalf("ingest %s: %v", e.key, err)
		}
	}

	got, err := s.ActivityStats(ctx, base, base.Add(72*time.Hour), "day")
	if err != nil {
		t.Fatalf("ActivityStats: %v", err)
	}
	want := []ActivityBucket{
		{TS: "2026-06-01", Total: 2, Succeeded: 1, Failed: 1},
		{TS: "2026-06-02", Total: 0, Succeeded: 0, Failed: 0}, // gap-filled
		{TS: "2026-06-03", Total: 3, Succeeded: 2, Failed: 0},
	}
	if len(got.Buckets) != len(want) {
		t.Fatalf("got %d buckets, want %d: %+v", len(got.Buckets), len(want), got.Buckets)
	}
	for i, w := range want {
		if got.Buckets[i] != w {
			t.Errorf("bucket %d = %+v, want %+v", i, got.Buckets[i], w)
		}
	}
}

func TestActivityStatsHourBucketAndGuards(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if _, _, err := s.Ingest(ctx, testEvent("h:1", base.Add(90*time.Minute))); err != nil {
		t.Fatal(err)
	}

	got, err := s.ActivityStats(ctx, base, base.Add(3*time.Hour), "hour")
	if err != nil {
		t.Fatalf("hour stats: %v", err)
	}
	if len(got.Buckets) != 3 || got.Buckets[0].TS != "2026-06-01T10:00" {
		t.Fatalf("hour buckets = %+v", got.Buckets)
	}
	if got.Buckets[1].Total != 1 { // the 11:30 event lands in the 11:00 bucket
		t.Errorf("11:00 bucket total = %d, want 1", got.Buckets[1].Total)
	}

	if _, err := s.ActivityStats(ctx, base, base.Add(3*time.Hour), "week"); err == nil {
		t.Error("invalid bucket must error")
	}
	// Oversized: hour buckets over a decade exceeds the cap.
	if _, err := s.ActivityStats(ctx, base, base.AddDate(10, 0, 0), "hour"); err == nil {
		t.Error("oversized window must error")
	}
}

func TestDeployStats(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	dep := kind(model.KindDeploy)

	seed := []*model.Event{
		testEvent("d:1", base.Add(1*time.Hour), dep, env("prod"), service("api"), status(model.StatusSucceeded)),
		testEvent("d:2", base.Add(2*time.Hour), dep, env("prod"), service("web"), status(model.StatusSucceeded)),
		testEvent("d:3", base.Add(3*time.Hour), dep, env("prod"), service("api"), status(model.StatusFailed)), // latest prod
		testEvent("d:4", base.Add(1*time.Hour), dep, env("staging"), service("api"), status(model.StatusSucceeded)),
		testEvent("d:5", base.Add(1*time.Hour), dep, env(""), service("api"), status(model.StatusSucceeded)),       // unmapped: excluded
		testEvent("d:6", base.Add(1*time.Hour), kind(model.KindBuild), env("prod"), status(model.StatusSucceeded)), // not a deploy: excluded
	}
	for _, e := range seed {
		if _, _, err := s.Ingest(ctx, e); err != nil {
			t.Fatalf("ingest %s: %v", e.DedupKey, err)
		}
	}

	got, err := s.DeployStats(ctx, base, base.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("DeployStats: %v", err)
	}
	if len(got.Envs) != 2 {
		t.Fatalf("got %d envs, want prod+staging only: %+v", len(got.Envs), got.Envs)
	}
	prod := got.Envs[0] // sorted by name: prod < staging
	if prod.Env != "prod" || prod.Total != 3 || prod.Succeeded != 2 || prod.Failed != 1 {
		t.Errorf("prod = %+v", prod)
	}
	if prod.Services != 2 { // api + web
		t.Errorf("prod services = %d, want 2", prod.Services)
	}
	if prod.LastStatus != "failed" || prod.LastTS == nil || !prod.LastTS.Equal(base.Add(3*time.Hour)) {
		t.Errorf("prod last = %v/%v, want failed @ +3h", prod.LastStatus, prod.LastTS)
	}
	staging := got.Envs[1]
	if staging.Env != "staging" || staging.Total != 1 || staging.Succeeded != 1 {
		t.Errorf("staging = %+v", staging)
	}
}
