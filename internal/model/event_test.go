package model

import (
	"strings"
	"testing"
	"time"
)

func validEvent() *Event {
	return &Event{
		ID:         NewID(),
		TS:         time.Now().UTC(),
		IngestedAt: time.Now().UTC(),
		Source:     SourceGeneric,
		Kind:       KindManual,
		Status:     StatusUnknown,
		Title:      "test event",
		DedupKey:   "test:1",
	}
}

func TestEventValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Event)
		wantErr string
	}{
		{"valid", func(e *Event) {}, ""},
		{"missing id", func(e *Event) { e.ID = "" }, "id is required"},
		{"missing ts", func(e *Event) { e.TS = time.Time{} }, "ts is required"},
		{"missing ingested_at", func(e *Event) { e.IngestedAt = time.Time{} }, "ingested_at is required"},
		{"missing title", func(e *Event) { e.Title = "" }, "title is required"},
		{"missing dedup_key", func(e *Event) { e.DedupKey = "" }, "dedup_key is required"},
		{"bad source", func(e *Event) { e.Source = "bitbucket" }, "invalid source"},
		{"bad kind", func(e *Event) { e.Kind = "release" }, "invalid kind"},
		{"bad status", func(e *Event) { e.Status = "done" }, "invalid status"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := validEvent()
			tt.mutate(ev)
			err := ev.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestStatusRank(t *testing.T) {
	if StatusRank(StatusUnknown) >= StatusRank(StatusStarted) {
		t.Fatal("unknown must rank below started")
	}
	if StatusRank(StatusStarted) >= StatusRank(StatusSucceeded) {
		t.Fatal("started must rank below succeeded")
	}
	if StatusRank(StatusSucceeded) != StatusRank(StatusFailed) {
		t.Fatal("succeeded and failed must rank equal (both terminal)")
	}
}

func TestTSRoundTripAndSortability(t *testing.T) {
	t1 := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(500 * time.Millisecond)

	s1, s2 := FormatTS(t1), FormatTS(t2)
	if s1 >= s2 {
		t.Fatalf("formatted timestamps must sort lexicographically: %q !< %q", s1, s2)
	}

	got, err := ParseTS(s2)
	if err != nil {
		t.Fatalf("ParseTS: %v", err)
	}
	if !got.Equal(t2) {
		t.Fatalf("round-trip mismatch: got %v want %v", got, t2)
	}
}

func TestNewIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id := NewID()
		if len(id) != 26 {
			t.Fatalf("ULID length = %d, want 26", len(id))
		}
		if seen[id] {
			t.Fatalf("duplicate ULID %s", id)
		}
		seen[id] = true
	}
}
