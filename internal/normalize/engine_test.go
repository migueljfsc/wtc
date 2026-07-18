package normalize

import (
	"testing"

	"github.com/migueljfsc/wtc/internal/model"
)

func mustEngine(t *testing.T, rules []Rule) *Engine {
	t.Helper()
	e, err := NewEngine(rules)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

func apply(t *testing.T, e *Engine, ev *model.Event, f Facts) *model.Event {
	t.Helper()
	if err := e.Apply(ev, f); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return ev
}

func TestEngineRules(t *testing.T) {
	rules := []Rule{
		{Match: RuleMatch{Source: "flux", Cluster: "prod"}, Set: RuleSet{Env: "prod"}},
		{Match: RuleMatch{Source: "github", Event: "workflow_run"}, Set: RuleSet{Kind: "build", Service: `{{ trimPrefix .Repo "org/" }}`}},
		{Match: RuleMatch{Source: "github", Paths: []string{"infrastructure/overlays/prod/**"}}, Set: RuleSet{Env: "prod"}},
		{Match: RuleMatch{Source: "github", Paths: []string{"infrastructure/overlays/staging/**"}}, Set: RuleSet{Env: "staging"}},
		{Match: RuleMatch{Repo: "org/app-*"}, Set: RuleSet{Actor: "fallback-actor", Env: "dev"}},
	}
	e := mustEngine(t, rules)

	t.Run("cluster to env", func(t *testing.T) {
		ev := apply(t, e, &model.Event{}, Facts{Source: "flux", Cluster: "prod"})
		if ev.Env != "prod" {
			t.Errorf("Env = %q, want prod", ev.Env)
		}
	})

	t.Run("workflow fact matches and templates", func(t *testing.T) {
		e := mustEngine(t, []Rule{
			{Match: RuleMatch{Source: "github", Workflow: "demo-*"}, Set: RuleSet{Service: "{{ .Workflow }}"}},
		})
		ev := apply(t, e, &model.Event{}, Facts{Source: "github", Workflow: "demo-api"})
		if ev.Service != "demo-api" {
			t.Errorf("Service = %q, want demo-api", ev.Service)
		}
		ev = apply(t, e, &model.Event{}, Facts{Source: "github", Workflow: "ci"})
		if ev.Service != "" {
			t.Errorf("Service = %q, non-demo workflow must not match", ev.Service)
		}
	})

	t.Run("template service from repo", func(t *testing.T) {
		ev := apply(t, e, &model.Event{}, Facts{Source: "github", Repo: "org/app-api", Event: "workflow_run"})
		if ev.Kind != model.KindBuild || ev.Service != "app-api" {
			t.Errorf("kind=%s service=%q, want build/app-api", ev.Kind, ev.Service)
		}
	})

	t.Run("path rule any-match", func(t *testing.T) {
		ev := apply(t, e, &model.Event{}, Facts{
			Source: "github", Repo: "org/app-api",
			Paths: []string{"README.md", "infrastructure/overlays/prod/kustomization.yaml"},
		})
		if ev.Env != "prod" {
			t.Errorf("Env = %q, want prod (any path matches)", ev.Env)
		}
	})

	t.Run("first writer wins across rules", func(t *testing.T) {
		// prod path rule (earlier) must beat the org/app-* env=dev fallback.
		ev := apply(t, e, &model.Event{}, Facts{
			Source: "github", Repo: "org/app-api",
			Paths: []string{"infrastructure/overlays/prod/x.yaml"},
		})
		if ev.Env != "prod" {
			t.Errorf("Env = %q, want prod (first rule wins)", ev.Env)
		}
		if ev.Actor != "fallback-actor" {
			t.Errorf("Actor = %q — later rules must still fill OTHER unset fields", ev.Actor)
		}
	})

	t.Run("parser-set fields not overwritten", func(t *testing.T) {
		ev := apply(t, e, &model.Event{Kind: model.KindMerge, Env: "staging"},
			Facts{Source: "github", Event: "workflow_run", Repo: "org/app-api"})
		if ev.Kind != model.KindMerge || ev.Env != "staging" {
			t.Errorf("kind=%s env=%q — rules must not clobber set fields", ev.Kind, ev.Env)
		}
	})

	t.Run("truncated paths skip path rules", func(t *testing.T) {
		ev := apply(t, e, &model.Event{}, Facts{
			Source: "github", Repo: "other/repo", PathsTruncated: true,
			Paths: []string{"infrastructure/overlays/prod/x.yaml"}, // must be ignored
		})
		if ev.Env != "" {
			t.Errorf("Env = %q, want \"\" (truncated list must not route)", ev.Env)
		}
	})

	t.Run("no match leaves env empty", func(t *testing.T) {
		ev := apply(t, e, &model.Event{}, Facts{Source: "github", Repo: "other/repo", Paths: []string{"src/main.go"}})
		if ev.Env != "" {
			t.Errorf("Env = %q, want \"\" (never guess)", ev.Env)
		}
	})

	t.Run("repo defaults from fact, not inferred by rules", func(t *testing.T) {
		ev := apply(t, e, &model.Event{}, Facts{Source: "github", Repo: "acme/storefront"})
		if ev.Repo != "acme/storefront" {
			t.Errorf("Repo = %q, want acme/storefront (raw fact persisted)", ev.Repo)
		}
	})

	t.Run("empty repo fact leaves repo empty", func(t *testing.T) {
		ev := apply(t, e, &model.Event{}, Facts{Source: "flux", Cluster: "prod"})
		if ev.Repo != "" {
			t.Errorf("Repo = %q, want \"\" (cluster-side events carry no repo)", ev.Repo)
		}
	})

	t.Run("invalid kind from rule ignored", func(t *testing.T) {
		bad := mustEngine(t, []Rule{{Match: RuleMatch{Source: "github"}, Set: RuleSet{Kind: "notakind"}}})
		ev := apply(t, bad, &model.Event{}, Facts{Source: "github"})
		if ev.Kind != "" {
			t.Errorf("Kind = %q, want unchanged", ev.Kind)
		}
	})
}

func TestEngineRedactsAfterRules(t *testing.T) {
	e := mustEngine(t, nil)
	ev := &model.Event{Title: "set password=hunter2"}
	_ = apply(t, e, ev, Facts{})
	if ev.Title != "set password=[REDACTED]" {
		t.Errorf("Title = %q — Apply must end with redaction", ev.Title)
	}
}

func TestCompileGlob(t *testing.T) {
	tests := []struct {
		pattern, s string
		want       bool
	}{
		{"infrastructure/overlays/prod/**", "infrastructure/overlays/prod/deploy.yaml", true},
		{"infrastructure/overlays/prod/**", "infrastructure/overlays/prod/a/b/c.yaml", true},
		{"infrastructure/overlays/prod/**", "infrastructure/overlays/staging/deploy.yaml", false},
		{"org/app-*", "org/app-api", true},
		{"org/app-*", "org/app-api/nested", false}, // * stops at /
		{"org/**", "org/app-api/nested", true},
		{"exact", "exact", true},
		{"exact", "exact-not", false},
		{"a.b", "aXb", false}, // dot is literal
	}
	for _, tt := range tests {
		re, err := CompileGlob(tt.pattern)
		if err != nil {
			t.Fatalf("CompileGlob(%q): %v", tt.pattern, err)
		}
		if got := re.MatchString(tt.s); got != tt.want {
			t.Errorf("glob %q vs %q = %v, want %v", tt.pattern, tt.s, got, tt.want)
		}
	}
}

func TestNewEngineErrors(t *testing.T) {
	if _, err := NewEngine([]Rule{{Set: RuleSet{Service: "{{ .Broken"}}}); err == nil {
		t.Error("bad template: want error at NewEngine time")
	}
}
