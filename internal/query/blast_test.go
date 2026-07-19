package query

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/store"
)

// Blast fixture: an incident timeline. An alert fires on api in prod; the
// window before it holds a failed prod api deploy (the culprit), an earlier
// succeeded prod api deploy, a closer-but-wrong-env staging deploy, and an
// unrelated merge. After a deploy, the alert itself is the "effects" answer.

var bt0 = time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC) // alert instant

type blastSeed struct {
	st    *store.Store
	alert *model.Event
	// suspects by mnemonic, for readable assertions
	failedProdDeploy, okProdDeploy, stagingDeploy, merge *model.Event
}

func seedBlast(t *testing.T) blastSeed {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	mk := func(dedup string, ts time.Time, mut func(*model.Event)) *model.Event {
		ev := &model.Event{
			ID: model.NewID(), TS: ts, IngestedAt: ts,
			Source: model.SourceGitHub, Kind: model.KindDeploy,
			Status: model.StatusSucceeded, Title: dedup, DedupKey: dedup,
		}
		mut(ev)
		if _, _, err := st.Ingest(context.Background(), ev); err != nil {
			t.Fatalf("seed %s: %v", dedup, err)
		}
		return ev
	}

	s := blastSeed{st: st}
	s.okProdDeploy = mk("flux:prod:api:ok", bt0.Add(-90*time.Minute), func(e *model.Event) {
		e.Source, e.Env, e.Service = model.SourceFlux, "prod", "api"
	})
	s.merge = mk("gh:pr:o/r:41", bt0.Add(-40*time.Minute), func(e *model.Event) {
		e.Kind, e.Env, e.Service = model.KindMerge, "", "" // env not inferred — a plain merge to main
	})
	s.failedProdDeploy = mk("flux:prod:api:fail", bt0.Add(-8*time.Minute), func(e *model.Event) {
		e.Source, e.Env, e.Service = model.SourceFlux, "prod", "api"
		e.Status = model.StatusFailed
	})
	s.stagingDeploy = mk("flux:staging:web:ok", bt0.Add(-2*time.Minute), func(e *model.Event) {
		e.Source, e.Env, e.Service = model.SourceFlux, "staging", "web"
	})
	s.alert = mk("am:HighErrorRate:prod:api", bt0, func(e *model.Event) {
		e.Source, e.Kind, e.Env, e.Service = model.SourceAlertmanager, model.KindAlert, "prod", "api"
		e.Status = model.StatusFailed
	})
	// An old deploy outside every window used in the tests.
	mk("flux:prod:api:ancient", bt0.Add(-26*time.Hour), func(e *model.Event) {
		e.Source, e.Env, e.Service = model.SourceFlux, "prod", "api"
	})
	return s
}

func suspectIDs(r *BlastReport) []string {
	ids := make([]string, len(r.Suspects))
	for i, s := range r.Suspects {
		ids[i] = s.Event.ID
	}
	return ids
}

func TestBlastCauses(t *testing.T) {
	s := seedBlast(t)
	r, err := Blast(context.Background(), s.st, BlastInput{Anchor: s.alert, TS: s.alert.TS})
	if err != nil {
		t.Fatal(err)
	}

	if r.Direction != DirectionCauses {
		t.Fatalf("direction = %q, want causes", r.Direction)
	}
	want := []string{s.failedProdDeploy.ID, s.okProdDeploy.ID, s.stagingDeploy.ID, s.merge.ID}
	got := suspectIDs(r)
	if len(got) != len(want) {
		t.Fatalf("suspects = %d, want %d (%v)", len(got), len(want), r.Suspects)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rank %d = %s, want %s (suspects %+v)", i, got[i], want[i], r.Suspects)
		}
	}

	// The failed same-env same-service deploy is the top suspect and its
	// reasons say why.
	top := r.Suspects[0]
	joined := strings.Join(top.Reasons, " · ")
	for _, want := range []string{"before", "same env (prod)", "same service (api)", "deploy", "failed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("top suspect reasons %q missing %q", joined, want)
		}
	}
	// Closer-but-wrong-env must not outrank same-env: staging deploy scored
	// below both prod deploys despite being nearest to the alert.
	if r.Suspects[2].Event.ID != s.stagingDeploy.ID {
		t.Errorf("staging deploy not at rank 2: %+v", r.Suspects)
	}
	// The alert itself is never a suspect.
	for _, id := range got {
		if id == s.alert.ID {
			t.Error("anchor alert listed as its own suspect")
		}
	}
}

func TestBlastEffects(t *testing.T) {
	s := seedBlast(t)
	r, err := Blast(context.Background(), s.st, BlastInput{Anchor: s.failedProdDeploy, TS: s.failedProdDeploy.TS})
	if err != nil {
		t.Fatal(err)
	}
	if r.Direction != DirectionEffects {
		t.Fatalf("direction = %q, want effects", r.Direction)
	}
	if len(r.Suspects) != 1 || r.Suspects[0].Event.ID != s.alert.ID {
		t.Fatalf("effects of the failed deploy = %+v, want just the alert", r.Suspects)
	}
	joined := strings.Join(r.Suspects[0].Reasons, " · ")
	for _, want := range []string{"after", "same env (prod)", "same service (api)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("effect reasons %q missing %q", joined, want)
		}
	}
}

func TestBlastBareTS(t *testing.T) {
	s := seedBlast(t)

	// No env context: same-env signal off, and the report says so.
	r, err := Blast(context.Background(), s.st, BlastInput{TS: bt0})
	if err != nil {
		t.Fatal(err)
	}
	if r.Direction != DirectionCauses {
		t.Fatalf("bare ts direction = %q, want causes", r.Direction)
	}
	noted := false
	for _, n := range r.Notes {
		if strings.Contains(n, "same-env signal is disabled") {
			noted = true
		}
	}
	if !noted {
		t.Errorf("no env-disabled note in %v", r.Notes)
	}
	// Without the env signal recency + failed bump still put the failed
	// deploy first, and the near staging deploy outranks the 90m-old prod one.
	if len(r.Suspects) < 3 {
		t.Fatalf("suspects = %+v", r.Suspects)
	}
	if r.Suspects[0].Event.ID != s.failedProdDeploy.ID {
		t.Errorf("top without env context = %s, want the failed deploy", r.Suspects[0].Event.ID)
	}
	if r.Suspects[1].Event.ID != s.stagingDeploy.ID {
		t.Errorf("rank 1 without env context = %s, want the staging deploy (recency)", r.Suspects[1].Event.ID)
	}

	// With --env prod the prod deploys move back on top.
	r, err = Blast(context.Background(), s.st, BlastInput{TS: bt0, Env: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Suspects[0].Event.ID; got != s.failedProdDeploy.ID {
		t.Errorf("top with --env prod = %s, want the failed prod deploy", got)
	}
}

func TestBlastWindowAndLimit(t *testing.T) {
	s := seedBlast(t)

	// A 5m window sees only the staging deploy.
	r, err := Blast(context.Background(), s.st, BlastInput{Anchor: s.alert, TS: s.alert.TS, Window: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Suspects) != 1 || r.Suspects[0].Event.ID != s.stagingDeploy.ID {
		t.Fatalf("5m window suspects = %+v, want just the staging deploy", r.Suspects)
	}

	// Limit truncates after ranking.
	r, err = Blast(context.Background(), s.st, BlastInput{Anchor: s.alert, TS: s.alert.TS, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Suspects) != 2 || r.Suspects[0].Event.ID != s.failedProdDeploy.ID {
		t.Fatalf("limit 2 suspects = %+v", r.Suspects)
	}

	// An empty window is an answer, not an error.
	r, err = Blast(context.Background(), s.st, BlastInput{TS: bt0.Add(-24 * time.Hour), Env: "prod", Window: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Suspects) != 0 {
		t.Fatalf("empty window suspects = %+v", r.Suspects)
	}
	found := false
	for _, n := range r.Notes {
		if strings.Contains(n, "no changes in the window") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing empty-window note: %v", r.Notes)
	}
}
