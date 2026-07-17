package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// TestDoctorWebhookChurn asserts the unstable-dedup_key heuristic: many rows
// sharing (source,title,kind,status) landing in a tight window under DISTINCT
// dedup_keys are flagged, while legitimately-spaced distinct events are not.
func TestDoctorWebhookChurn(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	base := now.Add(-1 * time.Hour)
	model.RegisterSource("grafana") // mapping-webhook names are first-class sources
	model.RegisterSource("ci")

	// Churn: 6 rows, same title/kind/status, seconds apart, distinct keys —
	// the signature of a retrying sender with a timestamped dedup_key.
	for i := 0; i < 6; i++ {
		ev := testEvent(fmt.Sprintf("grafana:unstable:%d", i), base.Add(time.Duration(i)*time.Second),
			func(e *model.Event) { e.Source = "grafana"; e.Title = "DiskFull firing" })
		if _, _, err := s.Ingest(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	// Not churn: distinct titles (genuinely different changes), same window.
	for i := 0; i < 6; i++ {
		ev := testEvent(fmt.Sprintf("ci:run:%d", i), base.Add(time.Duration(i)*time.Second),
			func(e *model.Event) { e.Source = "ci"; e.Title = fmt.Sprintf("build #%d", i) })
		if _, _, err := s.Ingest(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}

	rep, err := s.Doctor(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	var found *WebhookChurn
	for i := range rep.WebhookChurn {
		if rep.WebhookChurn[i].Source == "grafana" {
			found = &rep.WebhookChurn[i]
		}
		if rep.WebhookChurn[i].Source == "ci" {
			t.Errorf("distinct-title source 'ci' should not be flagged as churn")
		}
	}
	if found == nil {
		t.Fatalf("expected churn flag for 'grafana', got %+v", rep.WebhookChurn)
	}
	if found.Rows < 5 || found.Title != "DiskFull firing" {
		t.Errorf("churn = %+v", *found)
	}
}
