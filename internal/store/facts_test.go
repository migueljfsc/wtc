package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// TestFactsPersistAcrossUpsert: the facts column survives ingest, and a
// status-advancing re-ingest WITHOUT facts (e.g. an older-format replay)
// never blanks what was recorded.
func TestFactsPersistAcrossUpsert(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	base := model.Event{
		TS: time.Now().UTC(), IngestedAt: time.Now().UTC(),
		Source: model.SourceGitHub, Kind: model.KindDeploy,
		Title: "deploy", DedupKey: "facts:1",
		Facts: `{"facts":{"source":"github"},"preset":{"kind":"deploy"}}`,
	}

	first := base
	first.ID = model.NewID()
	first.Status = model.StatusStarted
	if _, _, err := st.Ingest(context.Background(), &first); err != nil {
		t.Fatal(err)
	}

	second := base
	second.ID = model.NewID()
	second.Status = model.StatusSucceeded
	second.Facts = "" // completion arrives without facts
	if _, deduped, err := st.Ingest(context.Background(), &second); err != nil || !deduped {
		t.Fatalf("second ingest: deduped=%v err=%v", deduped, err)
	}

	events, _, err := st.ListEvents(context.Background(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("rows = %d, want 1", len(events))
	}
	if events[0].Status != model.StatusSucceeded {
		t.Errorf("status = %s, want succeeded", events[0].Status)
	}
	if events[0].Facts != base.Facts {
		t.Errorf("facts = %q, want the recorded ones preserved", events[0].Facts)
	}
}
