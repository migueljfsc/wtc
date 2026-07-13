package store

import (
	"context"
	"testing"
	"time"
)

func TestPollWatermark(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Unset → zero time, no error.
	got, err := s.PollWatermark(ctx, "org/app", "runs")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsZero() {
		t.Fatalf("unset watermark = %v, want zero", got)
	}

	t1 := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	if err := s.SetPollWatermark(ctx, "org/app", "runs", t1); err != nil {
		t.Fatal(err)
	}
	got, err = s.PollWatermark(ctx, "org/app", "runs")
	if err != nil || !got.Equal(t1) {
		t.Fatalf("watermark = %v err=%v, want %v", got, err, t1)
	}

	// Advance moves it forward.
	t2 := t1.Add(time.Hour)
	if err := s.SetPollWatermark(ctx, "org/app", "runs", t2); err != nil {
		t.Fatal(err)
	}
	// Regression attempt is ignored (monotonic).
	if err := s.SetPollWatermark(ctx, "org/app", "runs", t1); err != nil {
		t.Fatal(err)
	}
	got, _ = s.PollWatermark(ctx, "org/app", "runs")
	if !got.Equal(t2) {
		t.Fatalf("watermark = %v, want %v (must never move backward)", got, t2)
	}

	// Resources are independent.
	if err := s.SetPollWatermark(ctx, "org/app", "prs", t1); err != nil {
		t.Fatal(err)
	}
	got, _ = s.PollWatermark(ctx, "org/app", "prs")
	if !got.Equal(t1) {
		t.Fatalf("prs watermark = %v, want %v", got, t1)
	}
}
