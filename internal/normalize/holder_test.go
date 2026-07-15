package normalize

import (
	"testing"

	"github.com/migueljfsc/wtc/internal/model"
)

func TestEngineHolderSwap(t *testing.T) {
	a, err := NewEngine([]Rule{{Match: RuleMatch{Source: "flux"}, Set: RuleSet{Env: "before"}}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewEngineHolder(a)

	ev := &model.Event{}
	if err := h.Apply(ev, Facts{Source: "flux"}); err != nil {
		t.Fatal(err)
	}
	if ev.Env != "before" {
		t.Fatalf("Env = %q, want before", ev.Env)
	}

	// Hot-swap a rebuilt engine; subsequent Apply uses the new rules.
	b, err := NewEngine([]Rule{{Match: RuleMatch{Source: "flux"}, Set: RuleSet{Env: "after"}}})
	if err != nil {
		t.Fatal(err)
	}
	h.Swap(b)

	ev2 := &model.Event{}
	if err := h.Apply(ev2, Facts{Source: "flux"}); err != nil {
		t.Fatal(err)
	}
	if ev2.Env != "after" {
		t.Fatalf("after swap Env = %q, want after", ev2.Env)
	}
}
