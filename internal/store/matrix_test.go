package store

import (
	"context"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

func artifact(v string) func(*model.Event) { return func(e *model.Event) { e.Artifact = v } }

func TestMatrix(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	dep := kind(model.KindDeploy)
	ok := status(model.StatusSucceeded)

	seed := []*model.Event{
		// api: prod has an older v1 then newer v2 (latest wins); staging on v2; dev on v2.
		testEvent("m:1", base.Add(1*time.Hour), dep, ok, env("prod"), service("api"), artifact("api:v1")),
		testEvent("m:2", base.Add(5*time.Hour), dep, ok, env("prod"), service("api"), artifact("api:v2")),
		testEvent("m:3", base.Add(4*time.Hour), dep, ok, env("staging"), service("api"), artifact("api:v2")),
		testEvent("m:4", base.Add(4*time.Hour), dep, ok, env("dev"), service("api"), artifact("api:v2")),
		// web: only in dev (not yet promoted).
		testEvent("m:5", base.Add(2*time.Hour), dep, ok, env("dev"), service("web"), artifact("web:v9")),
		// a failed prod deploy must NOT become the current cell.
		testEvent("m:6", base.Add(6*time.Hour), dep, status(model.StatusFailed), env("prod"), service("api"), artifact("api:v3")),
		// ephemeral env excluded from the default column set.
		testEvent("m:7", base.Add(1*time.Hour), dep, ok, env("pr-7"), service("api"), artifact("api:pr")),
	}
	for _, e := range seed {
		if _, _, err := s.Ingest(ctx, e); err != nil {
			t.Fatalf("ingest %s: %v", e.DedupKey, err)
		}
	}

	m, err := s.Matrix(ctx, []string{"dev", "staging", "prod"}, time.Time{})
	if err != nil {
		t.Fatalf("Matrix: %v", err)
	}
	if len(m.Services) != 2 {
		t.Fatalf("got %d services, want api+web: %+v", len(m.Services), m.Services)
	}
	api := m.Services[0] // sorted: api < web
	if api.Service != "api" {
		t.Fatalf("first service = %s", api.Service)
	}
	// Current prod = latest SUCCEEDED (v2), not the later failed v3.
	if got := api.Cells["prod"].Artifact; got != "api:v2" {
		t.Errorf("prod cell = %q, want api:v2 (failed v3 ignored)", got)
	}
	if _, ok := api.Cells["staging"]; !ok {
		t.Error("api should have a staging cell")
	}
	web := m.Services[1]
	if _, inProd := web.Cells["prod"]; inProd {
		t.Error("web is only in dev; prod cell should be absent (not-yet-promoted)")
	}

	// Default envs exclude ephemeral pr-*.
	def, err := s.Matrix(ctx, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range def.Envs {
		if e == "pr-7" {
			t.Errorf("default matrix envs must exclude ephemeral: %v", def.Envs)
		}
	}
}

func matrixArtifact(m *Matrix, svc, env string) string {
	for _, r := range m.Services {
		if r.Service == svc {
			return r.Cells[env].Artifact
		}
	}
	return ""
}

func TestMatrixAsOf(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	dep := kind(model.KindDeploy)
	ok := status(model.StatusSucceeded)

	seed := []*model.Event{
		testEvent("a:1", base.Add(1*time.Hour), dep, ok, env("prod"), service("api"), artifact("api:v1")),
		testEvent("a:2", base.Add(5*time.Hour), dep, ok, env("prod"), service("api"), artifact("api:v2")),
		// staging's first-ever event is at +8h.
		testEvent("a:3", base.Add(8*time.Hour), dep, ok, env("staging"), service("api"), artifact("api:v2")),
	}
	for _, e := range seed {
		if _, _, err := s.Ingest(ctx, e); err != nil {
			t.Fatalf("ingest %s: %v", e.DedupKey, err)
		}
	}

	// As of +3h prod is still on v1 — v2 is not deployed until +5h.
	m, err := s.Matrix(ctx, []string{"prod"}, base.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := matrixArtifact(m, "api", "prod"); got != "api:v1" {
		t.Errorf("prod api as-of +3h = %q, want api:v1", got)
	}
	// As of +6h prod has advanced to v2.
	m2, err := s.Matrix(ctx, []string{"prod"}, base.Add(6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := matrixArtifact(m2, "api", "prod"); got != "api:v2" {
		t.Errorf("prod api as-of +6h = %q, want api:v2", got)
	}
	// As of +30m nothing has deployed yet.
	m0, err := s.Matrix(ctx, []string{"prod"}, base.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(m0.Services) != 0 {
		t.Errorf("matrix as-of +30m must be empty; got %+v", m0.Services)
	}
	// Default env discovery is point-in-time too: staging is first seen at +8h,
	// so an as-of +3h default grid must not invent it as a column.
	def, err := s.Matrix(ctx, nil, base.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range def.Envs {
		if e == "staging" {
			t.Errorf("default envs as-of +3h must exclude staging (first seen +8h): %v", def.Envs)
		}
	}
}
