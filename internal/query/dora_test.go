package query

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/store"
)

func doraStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func ingestEv(t *testing.T, st *store.Store, ev *model.Event) {
	t.Helper()
	ev.ID = model.NewID()
	if ev.IngestedAt.IsZero() {
		ev.IngestedAt = ev.TS
	}
	if ev.Status == "" {
		ev.Status = model.StatusSucceeded
	}
	if ev.Title == "" {
		ev.Title = ev.DedupKey
	}
	if _, _, err := st.Ingest(context.Background(), ev); err != nil {
		t.Fatalf("ingest %s: %v", ev.DedupKey, err)
	}
}

func doraGroup(groups []DORAGroup, key string) *DORAGroup {
	for i := range groups {
		if groups[i].Key == key {
			return &groups[i]
		}
	}
	return nil
}

func TestDORA(t *testing.T) {
	st := doraStore(t)
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	ms := func(v int64) *int64 { return &v }
	dep := model.SourceGeneric

	// prod (owner platform): 3 deploys — one failed, one clean-then-alerted,
	// one clean. staging (owner data): 1 clean deploy.
	ingestEv(t, st, &model.Event{DedupKey: "d1", Source: dep, Kind: model.KindDeploy, Env: "prod", Owner: "platform", Service: "api", TS: base})
	ingestEv(t, st, &model.Event{DedupKey: "d2", Source: dep, Kind: model.KindDeploy, Env: "prod", Owner: "platform", Service: "api", TS: base.Add(time.Hour), Status: model.StatusFailed})
	ingestEv(t, st, &model.Event{DedupKey: "d3", Source: dep, Kind: model.KindDeploy, Env: "prod", Owner: "platform", Service: "api", TS: base.Add(2 * time.Hour)})
	ingestEv(t, st, &model.Event{DedupKey: "s1", Source: dep, Kind: model.KindDeploy, Env: "staging", Owner: "data", Service: "worker", TS: base.Add(3 * time.Hour)})

	// A resolved alert 10m after d3 in prod → makes d3 a failure; 30-min MTTR.
	ingestEv(t, st, &model.Event{DedupKey: "a1", Source: model.SourceAlertmanager, Kind: model.KindAlert, Env: "prod", Owner: "platform", TS: base.Add(2*time.Hour + 10*time.Minute), DurationMS: ms(30 * 60 * 1000)})

	r, err := DORA(context.Background(), st, resolver(t), base.Add(-time.Hour), base.Add(4*time.Hour), time.Hour, store.AggScope{})
	if err != nil {
		t.Fatal(err)
	}

	// prod: 3 deploys, 2 failures (d2 failed outright, d3 alert-followed) → CFR 2/3.
	prod := doraGroup(r.ByEnv, "prod")
	if prod == nil || prod.Deploys != 3 || prod.Failures != 2 {
		t.Fatalf("prod = %+v, want 3 deploys / 2 failures", prod)
	}
	if prod.ChangeFailureRate < 0.66 || prod.ChangeFailureRate > 0.67 {
		t.Errorf("prod CFR = %v, want ~0.667", prod.ChangeFailureRate)
	}
	if prod.MTTRSeconds == nil || *prod.MTTRSeconds != 1800 {
		t.Errorf("prod MTTR = %v, want 1800s", prod.MTTRSeconds)
	}

	// staging: 1 clean deploy, no failures, no incidents.
	stg := doraGroup(r.ByEnv, "staging")
	if stg == nil || stg.Deploys != 1 || stg.Failures != 0 || stg.MTTRSeconds != nil {
		t.Errorf("staging = %+v, want 1 deploy / 0 failures / no MTTR", stg)
	}

	// owner grouping mirrors env here.
	if plat := doraGroup(r.ByOwner, "platform"); plat == nil || plat.Failures != 2 {
		t.Errorf("owner platform = %+v, want 2 failures", plat)
	}

	// overall: 4 deploys, 2 failures.
	if r.Overall.Deploys != 4 || r.Overall.Failures != 2 {
		t.Errorf("overall = %+v, want 4 deploys / 2 failures", r.Overall)
	}
	if r.WindowSeconds != 3600 {
		t.Errorf("window = %d, want 3600", r.WindowSeconds)
	}
}

// A deploy just outside the failure window is not attributed the later alert.
func TestDORAWindowBoundary(t *testing.T) {
	st := doraStore(t)
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	ingestEv(t, st, &model.Event{DedupKey: "d", Source: model.SourceGeneric, Kind: model.KindDeploy, Env: "prod", TS: base})
	// Alert 90 min later — outside a 60-min window.
	ingestEv(t, st, &model.Event{DedupKey: "a", Source: model.SourceAlertmanager, Kind: model.KindAlert, Env: "prod", TS: base.Add(90 * time.Minute)})

	r, err := DORA(context.Background(), st, resolver(t), base.Add(-time.Hour), base.Add(3*time.Hour), time.Hour, store.AggScope{})
	if err != nil {
		t.Fatal(err)
	}
	if prod := doraGroup(r.ByEnv, "prod"); prod == nil || prod.Failures != 0 {
		t.Errorf("prod = %+v, want 0 failures (alert outside window)", prod)
	}
}
