package alertmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/store"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../testdata/alertmanager/firing.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

var testNow = time.Date(2026, 7, 14, 14, 0, 0, 0, time.UTC)

func TestGoldenFiring(t *testing.T) {
	pairs, err := Normalize(fixture(t), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 {
		t.Fatalf("got %d events, want 1", len(pairs))
	}
	ev, facts := pairs[0].Event, pairs[0].Facts
	if err := ev.Validate(); err != nil {
		t.Fatalf("invalid: %v", err)
	}
	if ev.Source != model.SourceAlertmanager || ev.Kind != model.KindAlert {
		t.Errorf("source/kind = %s/%s", ev.Source, ev.Kind)
	}
	if ev.Status != model.StatusStarted {
		t.Errorf("status = %s, want started (firing)", ev.Status)
	}
	if ev.Title != "HighErrorRate: demo-api 5xx rate above 5%" {
		t.Errorf("title = %q", ev.Title)
	}
	if ev.Service != "demo-api" || ev.Cluster != "prod" || ev.Namespace != "prod" {
		t.Errorf("service/cluster/ns = %s/%s/%s", ev.Service, ev.Cluster, ev.Namespace)
	}
	if ev.DedupKey != "am:08e93059ab2baf96:2026-07-14T13:41:34Z" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if !ev.TS.Equal(time.Date(2026, 7, 14, 13, 41, 34, 0, time.UTC)) {
		t.Errorf("ts = %v, want startsAt", ev.TS)
	}
	if facts.Cluster != "prod" || facts.Reason != "critical" {
		t.Errorf("facts = %+v", facts)
	}
}

func TestResolvedClosesTheEpisode(t *testing.T) {
	// Resolved delivery: same fingerprint/startsAt, endsAt set. Built from
	// the captured fixture by flipping the documented status field.
	// The raw capture is compact JSON — normalize whitespace before editing.
	compact := strings.ReplaceAll(strings.ReplaceAll(string(fixture(t)), "\n", ""), `": `, `":`)
	resolved := strings.ReplaceAll(compact, `"status":"firing"`, `"status":"resolved"`)
	resolved = strings.ReplaceAll(resolved,
		`"endsAt":"0001-01-01T00:00:00Z"`, `"endsAt":"2026-07-14T13:51:34Z"`)
	if !strings.Contains(resolved, `"status":"resolved"`) {
		t.Fatal("fixture edit failed — capture format changed?")
	}
	raw := fixture(t)

	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	ingest := func(body string) {
		pairs, err := Normalize([]byte(body), testNow)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range pairs {
			if _, _, err := st.Ingest(t.Context(), p.Event); err != nil {
				t.Fatal(err)
			}
		}
	}
	ingest(string(raw))
	ingest(resolved)

	events, _, err := st.ListEvents(t.Context(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d rows, want 1 (episode upserts, not duplicates)", len(events))
	}
	ev := events[0]
	if ev.Status != model.StatusSucceeded {
		t.Errorf("status = %s, want succeeded (resolved)", ev.Status)
	}
	if ev.DurationMS == nil || *ev.DurationMS != (10*time.Minute).Milliseconds() {
		t.Errorf("duration = %v, want 10m", ev.DurationMS)
	}
	if !ev.TS.Equal(time.Date(2026, 7, 14, 13, 51, 34, 0, time.UTC)) {
		t.Errorf("ts = %v, want endsAt", ev.TS)
	}
}

func TestCorrelation_AlertFindsPrecedingDeploy(t *testing.T) {
	// P5 acceptance: the alert correlates to the deploy that preceded it.
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	alertTS := time.Date(2026, 7, 14, 13, 41, 34, 0, time.UTC)
	deploy := &model.Event{
		ID: model.NewID(), TS: alertTS.Add(-7 * time.Minute), IngestedAt: testNow,
		Source: model.SourceFlux, Kind: model.KindDeploy, Status: model.StatusSucceeded,
		Env: "prod", Service: "demo-api", Actor: "flux",
		Title:    "Kustomization flux-system/demo-api-prod: ReconciliationSucceeded",
		DedupKey: "flux:prod:k/f/demo-api-prod:r1:ok",
	}
	old := &model.Event{
		ID: model.NewID(), TS: alertTS.Add(-3 * time.Hour), IngestedAt: testNow,
		Source: model.SourceFlux, Kind: model.KindDeploy, Status: model.StatusSucceeded,
		Env: "prod", Service: "demo-web", Actor: "flux",
		Title:    "old deploy outside the window",
		DedupKey: "flux:prod:k/f/demo-web-prod:r0:ok",
	}
	for _, ev := range []*model.Event{deploy, old} {
		if _, _, err := st.Ingest(t.Context(), ev); err != nil {
			t.Fatal(err)
		}
	}
	pairs, err := Normalize(fixture(t), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.Ingest(t.Context(), pairs[0].Event); err != nil {
		t.Fatal(err)
	}

	// The around query: 30m window before the alert's ts.
	events, _, err := st.ListEvents(t.Context(), store.Filter{
		Since: alertTS.Add(-30 * time.Minute),
		Until: alertTS,
	})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, ev := range events {
		kinds = append(kinds, string(ev.Kind)+":"+ev.Service)
	}
	if len(events) != 2 { // the alert itself + the culprit deploy
		t.Fatalf("window = %v, want the alert + the preceding deploy only", kinds)
	}
	if events[1].Kind != model.KindDeploy || events[1].Service != "demo-api" {
		t.Errorf("preceding change = %v, want the demo-api deploy", kinds)
	}
}
