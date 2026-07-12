package main

import (
	"testing"
	"time"
)

func TestParseTimeRef(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{"2h", now.Add(-2 * time.Hour), false},
		{"30m", now.Add(-30 * time.Minute), false},
		{"7d", now.Add(-7 * 24 * time.Hour), false},
		{"1w", now.Add(-7 * 24 * time.Hour), false},
		{"90s", now.Add(-90 * time.Second), false},
		{"2026-07-01T00:00:00Z", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), false},
		{"yesterday", time.Time{}, true},
		{"2 h", time.Time{}, true},
		{"", time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseTimeRef(tt.in, now)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseTimeRef(%q): want error, got %v", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTimeRef(%q): %v", tt.in, err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("parseTimeRef(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
