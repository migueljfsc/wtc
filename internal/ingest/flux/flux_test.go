package flux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

const fixtureDir = "../../../testdata/flux"

func loadEvent(t *testing.T, name string) *Event {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	fe, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return fe
}

var testNow = time.Date(2026, 7, 13, 22, 0, 0, 0, time.UTC)

func TestGoldenKustomizationSucceeded(t *testing.T) {
	ev, facts := Normalize(loadEvent(t, "kustomization_reconcile_succeeded.json"), testNow)
	if err := ev.Validate(); err != nil {
		t.Fatalf("invalid: %v", err)
	}
	if ev.Source != model.SourceFlux || ev.Kind != model.KindDeploy || ev.Status != model.StatusSucceeded {
		t.Errorf("source/kind/status = %s/%s/%s", ev.Source, ev.Kind, ev.Status)
	}
	if ev.Cluster != "dev" {
		t.Errorf("cluster = %q, want dev (Alert eventMetadata)", ev.Cluster)
	}
	if ev.Namespace != "flux-system" || ev.Actor != "flux" {
		t.Errorf("ns/actor = %q/%q", ev.Namespace, ev.Actor)
	}
	if ev.Ref != "46b93c870014ed9ee43488f0463b0f0744ac327c" {
		t.Errorf("ref = %q — must be the bare sha extracted from master@sha1:… for the where join", ev.Ref)
	}
	if ev.DedupKey != "flux:dev:Kustomization/flux-system/podinfo:master@sha1:46b93c870014ed9ee43488f0463b0f0744ac327c:ReconciliationSucceeded" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if ev.Title != "Kustomization flux-system/podinfo: ReconciliationSucceeded" {
		t.Errorf("title = %q", ev.Title)
	}
	if !strings.Contains(ev.Payload, "Reconciliation finished") {
		t.Errorf("payload must carry the message: %q", ev.Payload)
	}
	if facts.Cluster != "dev" || facts.ObjectKind != "Kustomization" || facts.ObjectName != "podinfo" {
		t.Errorf("facts = %+v", facts)
	}
}

func TestGoldenKustomizationFailed(t *testing.T) {
	ev, _ := Normalize(loadEvent(t, "kustomization_artifact_failed.json"), testNow)
	if ev.Status != model.StatusFailed {
		t.Errorf("status = %s, want failed (severity error)", ev.Status)
	}
	if ev.Ref != "46b93c870014ed9ee43488f0463b0f0744ac327c" {
		t.Errorf("ref = %q — failures keep their revision", ev.Ref)
	}
	if !strings.Contains(ev.DedupKey, ":ArtifactFailed") {
		t.Errorf("dedup = %q must include reason", ev.DedupKey)
	}
}

func TestGoldenHelmReleaseInstalled(t *testing.T) {
	ev, facts := Normalize(loadEvent(t, "helmrelease_install_succeeded.json"), testNow)
	if ev.Status != model.StatusSucceeded || ev.Kind != model.KindDeploy {
		t.Errorf("kind/status = %s/%s", ev.Kind, ev.Status)
	}
	if ev.Ref != "" {
		t.Errorf("ref = %q — chart version is not a git sha", ev.Ref)
	}
	if ev.Artifact != "podinfo-helm@6.14.0" {
		t.Errorf("artifact = %q, want release@chart-version", ev.Artifact)
	}
	if facts.ObjectKind != "HelmRelease" || facts.ObjectName != "podinfo-helm" {
		t.Errorf("facts = %+v", facts)
	}
}

func TestProgressingDropped(t *testing.T) {
	// Reason string observed live on the demo stack; payload shape identical
	// to the captured success fixture.
	fe := loadEvent(t, "kustomization_reconcile_succeeded.json")
	fe.Reason = "Progressing"
	if ev, _ := Normalize(fe, testNow); ev != nil {
		t.Fatalf("Progressing must be dropped, got %+v", ev)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte(`{"severity":"info"}`)); err == nil {
		t.Error("event without involvedObject must be rejected")
	}
	if _, err := Parse([]byte(`not json`)); err == nil {
		t.Error("non-JSON must be rejected")
	}
}

func TestSuppressor(t *testing.T) {
	s := NewSuppressor(10 * time.Minute)
	t0 := testNow

	if s.Suppress("k1", t0) {
		t.Fatal("first sighting must not be suppressed")
	}
	if !s.Suppress("k1", t0.Add(time.Minute)) {
		t.Fatal("repeat within window must be suppressed")
	}
	if s.Suppress("k2", t0.Add(time.Minute)) {
		t.Fatal("different key must not be suppressed")
	}
	if s.Suppress("k1", t0.Add(11*time.Minute)) {
		t.Fatal("repeat after window must pass")
	}

	off := NewSuppressor(0)
	if off.Suppress("k1", t0) {
		t.Fatal("window 0 disables suppression (first call)")
	}
	if off.Suppress("k1", t0) {
		t.Fatal("window 0 disables suppression (repeat call)")
	}
}
