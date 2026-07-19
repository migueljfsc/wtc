package normalize

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

func explainRules(t *testing.T) *Engine {
	t.Helper()
	e, err := NewEngine([]Rule{
		{Match: RuleMatch{Source: "flux", Cluster: "dev"}, Set: RuleSet{Env: "dev"}},
		{Match: RuleMatch{ObjectKind: "Kustomization"}, Set: RuleSet{Service: "{{ .ObjectName }}"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestApplyRecordsFacts(t *testing.T) {
	e := explainRules(t)
	ev := &model.Event{
		ID: model.NewID(), TS: time.Now(), IngestedAt: time.Now(),
		Source: model.SourceFlux, Kind: model.KindDeploy, Status: model.StatusSucceeded,
		Title: "reconcile", DedupKey: "t", Actor: "flux",
	}
	f := Facts{Source: "flux", Cluster: "dev", ObjectKind: "Kustomization", ObjectName: "podinfo"}
	if err := e.Apply(ev, f); err != nil {
		t.Fatal(err)
	}
	if ev.Env != "dev" || ev.Service != "podinfo" {
		t.Fatalf("rules did not apply: env=%q service=%q", ev.Env, ev.Service)
	}
	if ev.Facts == "" {
		t.Fatal("Apply did not record facts")
	}

	gotFacts, preset, err := DecodeFactsRecord(ev.Facts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotFacts, f) {
		t.Errorf("facts round-trip: got %+v, want %+v", gotFacts, f)
	}
	// The preset holds what the normalizer set BEFORE rules ran — kind and
	// actor here, but not the rule-derived env/service.
	if preset["kind"] != "deploy" || preset["actor"] != "flux" {
		t.Errorf("preset = %v, want kind=deploy actor=flux", preset)
	}
	if _, ok := preset["env"]; ok {
		t.Errorf("preset contains rule-set env: %v", preset)
	}
}

func TestApplyRedactsFacts(t *testing.T) {
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	ev := &model.Event{
		ID: model.NewID(), TS: time.Now(), IngestedAt: time.Now(),
		Source: model.SourceFlux, Kind: model.KindDeploy, Status: model.StatusSucceeded,
		Title: "x", DedupKey: "t",
	}
	// A branch name carrying a GitHub PAT-shaped string must not reach the
	// stored facts verbatim.
	if err := e.Apply(ev, Facts{Branch: "ghp_abcdefghij1234567890ABCD"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ev.Facts, "ghp_abcdefghij") {
		t.Errorf("facts not redacted: %s", ev.Facts)
	}
}

func TestExplain(t *testing.T) {
	e := explainRules(t)
	f := Facts{Source: "flux", Cluster: "dev", ObjectKind: "Kustomization", ObjectName: "podinfo"}
	preset := map[string]string{"kind": "deploy", "actor": "flux"}

	traces, err := e.Explain(f, preset)
	if err != nil {
		t.Fatal(err)
	}
	byField := map[string]FieldTrace{}
	for _, tr := range traces {
		byField[tr.Field] = tr
	}

	env := byField["env"]
	if env.Origin != "rule" || env.Value != "dev" || env.RuleIndex == nil || *env.RuleIndex != 0 {
		t.Errorf("env trace = %+v, want rule 0 → dev", env)
	}
	if want := "source=flux cluster=dev"; env.RuleMatch != want {
		t.Errorf("env rule_match = %q, want %q", env.RuleMatch, want)
	}
	svc := byField["service"]
	if svc.Origin != "rule" || svc.Value != "podinfo" || svc.RuleIndex == nil || *svc.RuleIndex != 1 {
		t.Errorf("service trace = %+v, want rule 1 → podinfo", svc)
	}
	if k := byField["kind"]; k.Origin != "normalizer" || k.Value != "deploy" {
		t.Errorf("kind trace = %+v, want normalizer deploy", k)
	}
	if c := byField["cluster"]; c.Origin != "unmatched" || c.Value != "" {
		t.Errorf("cluster trace = %+v, want unmatched", c)
	}

	// First-writer-wins: a preset service must beat the matching rule.
	traces, err = e.Explain(f, map[string]string{"service": "handset"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range traces {
		if tr.Field == "service" && (tr.Origin != "normalizer" || tr.Value != "handset") {
			t.Errorf("preset service trace = %+v, want normalizer handset", tr)
		}
	}
}
