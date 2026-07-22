package query

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

// Seeded timeline reproducing the operator's real flow:
// build → push (dev, monorepo flux) → PR bump merged (staging) → reconcile →
// later PR bump (prod) → reconcile. Plus a service present in one env only
// and an artifact-carrying pair for in-sync comparison.

const (
	shaApp     = "aaa1111000000000000000000000000000000000" // app commit (built image)
	revStaging = "bbb2222000000000000000000000000000000000" // manifests revision, staging bump
	revProd    = "ccc3333000000000000000000000000000000000" // manifests revision, prod bump
)

var t0 = time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

func seed(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	mk := func(dedup string, ts time.Time, mut func(*model.Event)) {
		ev := &model.Event{
			ID: model.NewID(), TS: ts, IngestedAt: ts,
			Source: model.SourceGitHub, Kind: model.KindBuild,
			Status: model.StatusSucceeded, Title: dedup, DedupKey: dedup,
		}
		mut(ev)
		if _, _, err := st.Ingest(context.Background(), ev); err != nil {
			t.Fatalf("seed %s: %v", dedup, err)
		}
	}

	// BUILD of the app commit.
	mk("gh:run:o/r:1:1", t0, func(e *model.Event) {
		e.Service = "demo-api"
		e.Actor = "alice"
		e.Ref = shaApp
	})
	// Dev flow: the commit itself pushed; flux reconciles the same revision.
	mk("gh:push:o/r:"+shaApp, t0.Add(1*time.Minute), func(e *model.Event) {
		e.Kind = model.KindPush
		e.Actor = "alice"
		e.Ref = shaApp
	})
	mk("flux:dev:Kustomization/f/demo-api-dev:"+shaApp+":ok", t0.Add(3*time.Minute), func(e *model.Event) {
		e.Source = model.SourceFlux
		e.Kind = model.KindDeploy
		e.Env = "dev"
		e.Service = "demo-api"
		e.Actor = "flux"
		e.Ref = shaApp
	})
	// Staging promotion: PR bump merged (enriched payload), then reconcile.
	mk("gh:pr:o/r:10:merged", t0.Add(10*time.Minute), func(e *model.Event) {
		e.Kind = model.KindMerge
		e.Env = "staging"
		e.Actor = "alice"
		e.Ref = revStaging
		e.Payload = `{"image_bumps":[{"file":"demo/api/infrastructure/overlays/staging/kustomization.yaml","old":"sha-0000000","new":"sha-aaa1111"}]}`
	})
	mk("flux:staging:Kustomization/f/demo-api-staging:"+revStaging+":ok", t0.Add(12*time.Minute), func(e *model.Event) {
		e.Source = model.SourceFlux
		e.Kind = model.KindDeploy
		e.Env = "staging"
		e.Service = "demo-api"
		e.Actor = "flux"
		e.Ref = revStaging
	})
	// Prod promotion, two hours later.
	mk("gh:pr:o/r:11:merged", t0.Add(2*time.Hour), func(e *model.Event) {
		e.Kind = model.KindMerge
		e.Env = "prod"
		e.Actor = "bob"
		e.Ref = revProd
		e.Payload = `{"image_bumps":[{"file":"demo/api/infrastructure/overlays/prod/kustomization.yaml","old":"sha-0000000","new":"sha-aaa1111"}]}`
	})
	mk("flux:prod:Kustomization/f/demo-api-prod:"+revProd+":ok", t0.Add(2*time.Hour+5*time.Minute), func(e *model.Event) {
		e.Source = model.SourceFlux
		e.Kind = model.KindDeploy
		e.Env = "prod"
		e.Service = "demo-api"
		e.Actor = "flux"
		e.Ref = revProd
	})
	// demo-web: staging only (diff must flag it).
	mk("flux:staging:Kustomization/f/demo-web-staging:r1:ok", t0.Add(20*time.Minute), func(e *model.Event) {
		e.Source = model.SourceFlux
		e.Kind = model.KindDeploy
		e.Env = "staging"
		e.Service = "demo-web"
		e.Actor = "flux"
		e.Ref = "ddd4444000000000000000000000000000000000"
	})
	// demo-worker: helm-style artifacts, same version in both envs (in sync).
	for _, env := range []string{"staging", "prod"} {
		mk("flux:"+env+":HelmRelease/f/demo-worker:6.1.0:ok", t0.Add(30*time.Minute), func(e *model.Event) {
			e.Source = model.SourceFlux
			e.Kind = model.KindDeploy
			e.Env = env
			e.Service = "demo-worker"
			e.Actor = "flux"
			e.Artifact = "demo-worker@6.1.0"
		})
	}
	// A failed prod deploy for handoff, newer service for first-seen.
	mk("flux:prod:Kustomization/f/demo-web-prod:r2:fail", t0.Add(40*time.Minute), func(e *model.Event) {
		e.Source = model.SourceFlux
		e.Kind = model.KindDeploy
		e.Status = model.StatusFailed
		e.Env = "prod"
		e.Service = "demo-web"
		e.Actor = "flux"
		e.Ref = "eee5555000000000000000000000000000000000"
	})
	return st
}

func resolver(t *testing.T) *normalize.TagResolver {
	t.Helper()
	r, err := normalize.NewTagResolver(nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWhereFullJourney(t *testing.T) {
	st := seed(t)
	ctx := context.Background()

	for _, input := range []string{"aaa1111", shaApp, "sha-aaa1111", "ghcr.io/x/demo-api:sha-aaa1111"} {
		t.Run(input, func(t *testing.T) {
			r, err := Where(ctx, st, resolver(t), input)
			if err != nil {
				t.Fatal(err)
			}
			if r.Sha != shaApp {
				t.Errorf("sha = %q, want expanded full sha", r.Sha)
			}
			if len(r.Builds) != 1 || r.Builds[0].Service != "demo-api" {
				t.Fatalf("builds = %+v", r.Builds)
			}
			// push + two enriched merges
			if len(r.Intents) != 3 {
				t.Fatalf("intents = %d, want 3 (push + 2 merges): %+v", len(r.Intents), r.Intents)
			}

			envs := map[string]WhereEnv{}
			for _, e := range r.Envs {
				envs[e.Env] = e
			}
			if len(envs) != 3 {
				t.Fatalf("envs = %v, want dev/staging/prod", r.Envs)
			}

			stg := envs["staging"]
			if stg.Applied == nil || stg.Applied.Ref != revStaging {
				t.Fatalf("staging applied = %+v", stg.Applied)
			}
			if stg.Intent == nil || stg.Intent.DedupKey != "gh:pr:o/r:10:merged" {
				t.Fatalf("staging intent = %+v", stg.Intent)
			}
			if stg.LagMS == nil || *stg.LagMS != (2*time.Minute).Milliseconds() {
				t.Errorf("staging lag = %v, want 2m", stg.LagMS)
			}

			prod := envs["prod"]
			if prod.Intent == nil || prod.Intent.DedupKey != "gh:pr:o/r:11:merged" {
				t.Fatalf("prod intent = %+v", prod.Intent)
			}
			if prod.LagMS == nil || *prod.LagMS != (5*time.Minute).Milliseconds() {
				t.Errorf("prod lag = %v, want 5m", prod.LagMS)
			}

			dev := envs["dev"]
			if dev.Intent == nil || dev.Intent.Kind != model.KindPush {
				t.Fatalf("dev intent = %+v, want the push", dev.Intent)
			}
		})
	}

	// Unknown sha: empty journey, not an error.
	r, err := Where(ctx, st, resolver(t), "f0f0f0f")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Builds) != 0 || len(r.Envs) != 0 || len(r.Notes) == 0 {
		t.Errorf("unknown sha must yield explicit empty report: %+v", r)
	}

	// Unresolvable input (not a sha, matches no tag_pattern) is not an error:
	// it yields an empty journey with an explanatory note.
	ur, err := Where(ctx, st, resolver(t), "latest")
	if err != nil {
		t.Fatalf("unresolvable input must not error: %v", err)
	}
	if ur.Sha != "" || len(ur.Builds) != 0 || len(ur.Envs) != 0 || len(ur.Notes) == 0 {
		t.Errorf("unresolvable input must yield empty report with a note: %+v", ur)
	}
}

func TestDiffStagingProd(t *testing.T) {
	st := seed(t)
	r, err := Diff(context.Background(), st, "staging", "prod", time.Time{}, store.AggScope{})
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]DiffRow{}
	for _, row := range r.Rows {
		rows[row.Service] = row
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want demo-api/web/worker", r.Rows)
	}

	api := rows["demo-api"]
	if api.InSync {
		t.Error("demo-api revisions differ; must not be in sync")
	}
	if !api.RevisionOnly {
		t.Error("demo-api has no artifacts: must be flagged revision-only")
	}
	if api.A != "bbb2222" || api.B != "ccc3333" {
		t.Errorf("demo-api compare = %q vs %q", api.A, api.B)
	}
	if api.DriftSeconds == nil || *api.DriftSeconds != int64((2*time.Hour+5*time.Minute-12*time.Minute).Seconds()) {
		t.Errorf("demo-api drift = %v", api.DriftSeconds)
	}

	web := rows["demo-web"]
	if web.OnlyIn != "staging" {
		t.Errorf("demo-web only_in = %q, want staging (prod deploy failed)", web.OnlyIn)
	}

	worker := rows["demo-worker"]
	if !worker.InSync || worker.RevisionOnly {
		t.Errorf("demo-worker = %+v, want in-sync artifact compare", worker)
	}
}

func TestDiffAsOf(t *testing.T) {
	st := seed(t)
	ctx := context.Background()

	// As of +1h demo-api has reconciled to staging (+12m) but not yet to prod
	// (+2h5m), so the later drift hasn't happened — it reads as only-in-staging.
	r, err := Diff(ctx, st, "staging", "prod", t0.Add(time.Hour), store.AggScope{})
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]DiffRow{}
	for _, row := range r.Rows {
		rows[row.Service] = row
	}
	if api, ok := rows["demo-api"]; !ok || api.OnlyIn != "staging" {
		t.Errorf("demo-api as-of +1h = %+v, want only_in=staging (prod deploy not yet applied)", rows["demo-api"])
	}
	if w, ok := rows["demo-worker"]; !ok || !w.InSync {
		t.Errorf("demo-worker as-of +1h = %+v, want in sync (deployed to both by +30m)", rows["demo-worker"])
	}

	// Before demo-api's first successful deploy (+12m) it must not appear.
	early, err := Diff(ctx, st, "staging", "prod", t0.Add(5*time.Minute), store.AggScope{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range early.Rows {
		if row.Service == "demo-api" {
			t.Errorf("demo-api must be absent before its first successful deploy; got %+v", row)
		}
	}
}

func TestHandoffDigest(t *testing.T) {
	st := seed(t)
	r, err := Handoff(context.Background(), st, t0.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if r.DeploysByEnv["staging"].Total != 3 || r.DeploysByEnv["staging"].Failed != 0 {
		t.Errorf("staging stats = %+v", r.DeploysByEnv["staging"])
	}
	if r.DeploysByEnv["prod"].Total != 3 || r.DeploysByEnv["prod"].Failed != 1 {
		t.Errorf("prod stats = %+v", r.DeploysByEnv["prod"])
	}
	if len(r.Failures) != 1 || r.Failures[0].Service != "demo-web" {
		t.Errorf("failures = %+v", r.Failures)
	}
	if r.Unmapped != 2 { // build + push have no env
		t.Errorf("unmapped = %d, want 2", r.Unmapped)
	}
	if len(r.TopActors) == 0 || r.TopActors[0].Actor != "flux" {
		t.Errorf("top actors = %+v, want flux first", r.TopActors)
	}
	if len(r.NewServices) != 3 {
		t.Errorf("new services = %v, want all three (first ever seen in window)", r.NewServices)
	}

	md := r.Markdown(t0.Add(3 * time.Hour))
	for _, want := range []string{"# Change handoff", "prod", "3 deploys, 1 failed", "Top actors", "demo-web"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}
