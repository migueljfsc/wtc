package query

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/store"
)

func csContains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// The seeded timeline is one logical change (app sha aaa1111): build + push +
// two per-env bump merges + dev/staging/prod reconciles. They must collapse into
// a single changeset spanning all three envs, even though each env's deploy
// carries a different manifests revision.
func TestChangesets(t *testing.T) {
	st := seed(t)
	r, err := Changesets(context.Background(), st, resolver(t), t0.Add(-time.Hour), t0.Add(4*time.Hour), store.AggScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changesets) != 1 {
		t.Fatalf("got %d changesets, want 1: %+v", len(r.Changesets), r.Changesets)
	}
	cs := r.Changesets[0]
	if cs.Sha != shaApp {
		t.Errorf("sha = %q, want the full form %q", cs.Sha, shaApp)
	}
	if !reflect.DeepEqual(cs.Services, []string{"demo-api"}) {
		t.Errorf("services = %v, want [demo-api]", cs.Services)
	}
	if !reflect.DeepEqual(cs.Envs, []string{"dev", "prod", "staging"}) {
		t.Errorf("envs = %v, want [dev prod staging]", cs.Envs)
	}
	for _, k := range []string{"build", "push", "merge", "deploy"} {
		if !csContains(cs.Kinds, k) {
			t.Errorf("kinds %v missing %s", cs.Kinds, k)
		}
	}
	if !cs.Deployed || cs.Failed {
		t.Errorf("deployed=%v failed=%v, want deployed=true failed=false", cs.Deployed, cs.Failed)
	}
	if cs.Events != 7 {
		t.Errorf("events = %d, want 7 (build+push+2 merges+3 deploys)", cs.Events)
	}
}

// A build plus a failed deploy carrying the same sha in its artifact tag folds
// into one changeset flagged failed, with no env reached.
func TestChangesetFailedRollup(t *testing.T) {
	st := doraStore(t)
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	ingestEv(t, st, &model.Event{DedupKey: "b", Source: model.SourceGitHub, Kind: model.KindBuild, Service: "api", Ref: "abcdef1234567890abcdef1234567890abcdef12", TS: base})
	ingestEv(t, st, &model.Event{DedupKey: "d", Source: model.SourceFlux, Kind: model.KindDeploy, Env: "prod", Service: "api", Status: model.StatusFailed, Artifact: "registry/api:sha-abcdef1", TS: base.Add(time.Minute)})

	r, err := Changesets(context.Background(), st, resolver(t), base.Add(-time.Hour), base.Add(time.Hour), store.AggScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changesets) != 1 {
		t.Fatalf("want 1 changeset, got %d: %+v", len(r.Changesets), r.Changesets)
	}
	cs := r.Changesets[0]
	if !cs.Failed {
		t.Error("a failed deploy must set Failed=true")
	}
	if cs.Deployed {
		t.Error("a failed deploy is not a succeeded deploy")
	}
	if len(cs.Envs) != 0 {
		t.Errorf("envs = %v, want none (the deploy failed)", cs.Envs)
	}
	if cs.Events != 2 {
		t.Errorf("events = %d, want 2 (build + failed deploy)", cs.Events)
	}
}

// The scope narrows changesets at the change level: keep changes that reached a
// scoped env / touched a scoped service, without dropping their env-less build.
func TestChangesetsScoped(t *testing.T) {
	st := seed(t)
	ctx := context.Background()
	win := func(scope store.AggScope) []Changeset {
		r, err := Changesets(ctx, st, resolver(t), t0.Add(-time.Hour), t0.Add(4*time.Hour), scope)
		if err != nil {
			t.Fatal(err)
		}
		return r.Changesets
	}

	// The seed's one changeset touches demo-api across dev/staging/prod.
	if got := win(store.AggScope{Services: []string{"demo-api"}}); len(got) != 1 {
		t.Errorf("service=demo-api => %d changesets, want 1", len(got))
	}
	if got := win(store.AggScope{Services: []string{"demo-web"}}); len(got) != 0 {
		t.Errorf("service=demo-web => %d changesets, want 0 (not part of a changeset)", len(got))
	}
	if got := win(store.AggScope{Envs: []string{"prod"}}); len(got) != 1 {
		t.Errorf("env=prod => %d changesets, want 1 (reached prod)", len(got))
	}
	if got := win(store.AggScope{Envs: []string{"canary"}}); len(got) != 0 {
		t.Errorf("env=canary => %d changesets, want 0", len(got))
	}
	// Its build carries no env, but the change still survives an env scope —
	// filtering is change-level, and the build's kind is still present.
	cs := win(store.AggScope{Envs: []string{"prod"}})[0]
	if !csContains(cs.Kinds, "build") {
		t.Errorf("env-scoped change lost its build event: kinds=%v", cs.Kinds)
	}
}
