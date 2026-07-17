package mapping

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

var testNow = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

// fixtureRoot unmarshals a captured webhook body into the parsed-JSON root the
// mapper renders against. Fixtures under testdata/webhook were captured live
// (Grafana 11.3 test contact point; Jenkins Notification Plugin serialized by
// its own classes) — see CHANGELOG P14 for provenance.
func fixtureRoot(t *testing.T, name string) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../../testdata/webhook", name))
	if err != nil {
		t.Fatal(err)
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return root
}

// compileOne compiles a single webhook (usually preset+auth) and returns its
// mapper, failing the test on any compile error.
func compileOne(t *testing.T, w Webhook) *Mapper {
	t.Helper()
	mappers, err := Compile([]Webhook{w})
	if err != nil {
		t.Fatalf("compile %s: %v", w.Name, err)
	}
	model.RegisterSource(model.Source(w.Name)) // so ev.Validate() accepts the source
	return mappers[w.Name]
}

func TestGrafanaPresetFiring(t *testing.T) {
	m := compileOne(t, Webhook{Name: "grafana", Preset: "grafana", Auth: Auth{Token: "s"}})
	pf, err := m.Normalize(fixtureRoot(t, "grafana-firing.json"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	ev := pf.Event
	if err := ev.Validate(); err != nil {
		t.Fatalf("invalid event: %v", err)
	}
	if ev.Source != "grafana" || ev.Kind != model.KindAlert {
		t.Errorf("source/kind = %s/%s", ev.Source, ev.Kind)
	}
	if ev.Status != model.StatusStarted {
		t.Errorf("status = %s, want started (firing)", ev.Status)
	}
	if ev.Title != "[FIRING:1] HighLatency prod-eks prod Grafana prod api critical" {
		t.Errorf("title = %q", ev.Title)
	}
	if ev.Env != "prod" {
		t.Errorf("env = %q, want prod", ev.Env)
	}
	if ev.DedupKey != "grafana:60c3ff6253477735:2026-07-17T09:01:00.318933091Z" {
		t.Errorf("dedup_key = %q", ev.DedupKey)
	}
	if ev.URL != "http://localhost:3000/" {
		t.Errorf("url = %q", ev.URL)
	}
	if pf.Facts.Cluster != "prod-eks" || pf.Facts.Namespace != "prod" || pf.Facts.Reason != "critical" {
		t.Errorf("facts = %+v", pf.Facts)
	}
	// ts came from the alert startsAt, not testNow.
	if got := ev.TS.Format(time.RFC3339Nano); got != "2026-07-17T09:01:00.318933091Z" {
		t.Errorf("ts = %s", got)
	}
}

// TestGrafanaPresetResolvedEpisode asserts firing and resolved deliveries of
// the SAME alert episode share a dedup_key (start fingerprint+startsAt) and
// move the status from started to succeeded — the episode is one upserted row.
func TestGrafanaPresetResolvedEpisode(t *testing.T) {
	m := compileOne(t, Webhook{Name: "grafana", Preset: "grafana", Auth: Auth{Token: "s"}})
	firing, _ := m.Normalize(fixtureRoot(t, "grafana-firing.json"), testNow)
	resolved, err := m.Normalize(fixtureRoot(t, "grafana-resolved.json"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if firing.Event.DedupKey != resolved.Event.DedupKey {
		t.Errorf("episode keys differ: %q vs %q", firing.Event.DedupKey, resolved.Event.DedupKey)
	}
	if resolved.Event.Status != model.StatusSucceeded {
		t.Errorf("resolved status = %s, want succeeded", resolved.Event.Status)
	}
	// resolved ts is the alert endsAt.
	if got := resolved.Event.TS.Format(time.RFC3339Nano); got != "2026-07-17T09:12:30.318933091Z" {
		t.Errorf("resolved ts = %s", got)
	}
}

// TestJenkinsPresetLifecycle asserts STARTED then COMPLETED of the same build
// share the dedup_key (job:number) and move started → succeeded — trap #5.
func TestJenkinsPresetLifecycle(t *testing.T) {
	m := compileOne(t, Webhook{Name: "jenkins", Preset: "jenkins", Auth: Auth{Token: "s"}})
	started, err := m.Normalize(fixtureRoot(t, "jenkins-started.json"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := m.Normalize(fixtureRoot(t, "jenkins-completed.json"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := started.Event.Validate(); err != nil {
		t.Fatalf("invalid: %v", err)
	}
	if started.Event.Kind != model.KindBuild {
		t.Errorf("kind = %s, want build", started.Event.Kind)
	}
	if started.Event.Status != model.StatusStarted {
		t.Errorf("started status = %s", started.Event.Status)
	}
	if completed.Event.Status != model.StatusSucceeded {
		t.Errorf("completed status = %s, want succeeded", completed.Event.Status)
	}
	if started.Event.DedupKey != "jenkins:api:3" || completed.Event.DedupKey != "jenkins:api:3" {
		t.Errorf("dedup keys = %q / %q", started.Event.DedupKey, completed.Event.DedupKey)
	}
	if started.Event.Title != "Jenkins api #3" {
		t.Errorf("title = %q", started.Event.Title)
	}
	if started.Event.Ref != "9f1c2ab3d4e5f60718293a4b5c6d7e8f90123456" {
		t.Errorf("ref = %q", started.Event.Ref)
	}
	if started.Facts.Branch != "origin/main" || started.Facts.Repo != "api" {
		t.Errorf("facts = %+v", started.Facts)
	}
}

func TestCompileErrors(t *testing.T) {
	cases := []struct {
		name string
		w    Webhook
	}{
		{"no-name", Webhook{Auth: Auth{Token: "s"}, DedupKey: "k", Mapping: FieldTemplates{Kind: "build", Title: "t"}}},
		{"builtin-collision", Webhook{Name: "github", Auth: Auth{Token: "s"}, DedupKey: "k", Mapping: FieldTemplates{Kind: "build", Title: "t"}}},
		{"no-auth", Webhook{Name: "x", DedupKey: "k", Mapping: FieldTemplates{Kind: "build", Title: "t"}}},
		{"both-auth", Webhook{Name: "x", Auth: Auth{Token: "s", HMAC: &HMACAuth{Secret: "z", Header: "H"}}, DedupKey: "k", Mapping: FieldTemplates{Kind: "build", Title: "t"}}},
		{"no-dedup", Webhook{Name: "x", Auth: Auth{Token: "s"}, Mapping: FieldTemplates{Kind: "build", Title: "t"}}},
		{"no-kind", Webhook{Name: "x", Auth: Auth{Token: "s"}, DedupKey: "k", Mapping: FieldTemplates{Title: "t"}}},
		{"no-title", Webhook{Name: "x", Auth: Auth{Token: "s"}, DedupKey: "k", Mapping: FieldTemplates{Kind: "build"}}},
		{"bad-template", Webhook{Name: "x", Auth: Auth{Token: "s"}, DedupKey: "{{ .a", Mapping: FieldTemplates{Kind: "build", Title: "t"}}},
		{"unknown-preset", Webhook{Name: "x", Preset: "nope", Auth: Auth{Token: "s"}}},
		{"unknown-fact", Webhook{Name: "x", Auth: Auth{Token: "s"}, DedupKey: "k", Mapping: FieldTemplates{Kind: "build", Title: "t"}, Facts: map[string]string{"bogus": "v"}}},
		{"bad-hmac-algo", Webhook{Name: "x", Auth: Auth{HMAC: &HMACAuth{Secret: "z", Header: "H", Algo: "md5"}}, DedupKey: "k", Mapping: FieldTemplates{Kind: "build", Title: "t"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Compile([]Webhook{c.w}); err == nil {
				t.Errorf("expected compile error for %s", c.name)
			}
		})
	}
}

func TestDuplicateNames(t *testing.T) {
	w := Webhook{Name: "dup", Auth: Auth{Token: "s"}, DedupKey: "k", Mapping: FieldTemplates{Kind: "build", Title: "t"}}
	if _, err := Compile([]Webhook{w, w}); err == nil {
		t.Error("expected duplicate-name error")
	}
}

// TestNormalizeErrors covers the runtime footguns doctor must surface: an
// unstable/empty dedup_key and an eval error.
func TestNormalizeErrors(t *testing.T) {
	// dedup_key renders empty (missing field, missingkey=zero).
	m := compileOne(t, Webhook{Name: "empty", Auth: Auth{Token: "s"},
		DedupKey: "{{ .absent }}", Mapping: FieldTemplates{Kind: "build", Title: "t"}})
	if _, err := m.Normalize(map[string]any{}, testNow); err == nil {
		t.Error("expected empty dedup_key error")
	}

	// title renders empty.
	m2 := compileOne(t, Webhook{Name: "notitle", Auth: Auth{Token: "s"},
		DedupKey: "k", Mapping: FieldTemplates{Kind: "build", Title: "{{ .absent }}"}})
	if _, err := m2.Normalize(map[string]any{}, testNow); err == nil {
		t.Error("expected empty title error")
	}

	// eval error: field access on a scalar.
	m3 := compileOne(t, Webhook{Name: "evalerr", Auth: Auth{Token: "s"},
		DedupKey: "{{ .x.y }}", Mapping: FieldTemplates{Kind: "build", Title: "t"}})
	if _, err := m3.Normalize(map[string]any{"x": "scalar"}, testNow); err == nil {
		t.Error("expected eval error")
	}
}

// TestPresetOverride asserts an operator can override a preset field while
// inheriting the rest.
func TestPresetOverride(t *testing.T) {
	m := compileOne(t, Webhook{Name: "g2", Preset: "grafana", Auth: Auth{Token: "s"},
		Mapping: FieldTemplates{Title: "custom"}})
	pf, err := m.Normalize(fixtureRoot(t, "grafana-firing.json"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if pf.Event.Title != "custom" {
		t.Errorf("override title = %q", pf.Event.Title)
	}
	if pf.Event.Kind != model.KindAlert { // inherited from preset
		t.Errorf("inherited kind = %q", pf.Event.Kind)
	}
}
