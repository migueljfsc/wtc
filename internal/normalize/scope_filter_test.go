package normalize

import "testing"

func TestScopeFilterPermit(t *testing.T) {
	f := func(ns, name, kind, cluster, project string) Facts {
		return Facts{Namespace: ns, ObjectName: name, ObjectKind: kind, Cluster: cluster, Project: project}
	}

	tests := []struct {
		name   string
		filter ScopeFilter
		facts  Facts
		want   bool
	}{
		{
			name:   "zero filter permits everything",
			filter: ScopeFilter{},
			facts:  f("flux-system", "cert-manager", "HelmRelease", "prod", ""),
			want:   true,
		},
		{
			name:   "empty allow, no deny match → allow all",
			filter: ScopeFilter{Deny: []ScopeMatch{{Namespace: "kube-*"}}},
			facts:  f("apps", "payments", "Kustomization", "prod", ""),
			want:   true,
		},
		{
			name:   "deny wins over a matching allow (broad allow, specific deny)",
			filter: ScopeFilter{Allow: []ScopeMatch{{Namespace: "**"}}, Deny: []ScopeMatch{{ObjectName: "*-canary"}}},
			facts:  f("apps", "payments-canary", "Kustomization", "prod", ""),
			want:   false,
		},
		{
			name:   "allow present, no match → drop",
			filter: ScopeFilter{Allow: []ScopeMatch{{Namespace: "prod-*"}}},
			facts:  f("staging-eu", "web", "Kustomization", "staging", ""),
			want:   false,
		},
		{
			name:   "allow present, match → keep",
			filter: ScopeFilter{Allow: []ScopeMatch{{Namespace: "prod-*"}}},
			facts:  f("prod-eu", "web", "Kustomization", "prod", ""),
			want:   true,
		},
		{
			name:   "AND within entry: namespace matches but kind does not → no match → drop",
			filter: ScopeFilter{Allow: []ScopeMatch{{Namespace: "apps", ObjectKind: "HelmRelease"}}},
			facts:  f("apps", "web", "Kustomization", "prod", ""),
			want:   false,
		},
		{
			name:   "AND within entry: all set fields match → keep",
			filter: ScopeFilter{Allow: []ScopeMatch{{Namespace: "apps", ObjectKind: "Kustomization"}}},
			facts:  f("apps", "web", "Kustomization", "prod", ""),
			want:   true,
		},
		{
			name:   "OR across entries: second allow matches → keep",
			filter: ScopeFilter{Allow: []ScopeMatch{{Namespace: "prod-*"}, {ObjectName: "payments"}}},
			facts:  f("apps", "payments", "Kustomization", "prod", ""),
			want:   true,
		},
		{
			name:   "argocd project match",
			filter: ScopeFilter{Allow: []ScopeMatch{{Project: "team-a"}}},
			facts:  f("app-ns", "checkout", "Application", "", "team-a"),
			want:   true,
		},
		{
			name:   "constrained namespace never matches an event with no namespace",
			filter: ScopeFilter{Allow: []ScopeMatch{{Namespace: "prod-*"}}},
			facts:  f("", "web", "Kustomization", "", ""),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs, err := tt.filter.Compile()
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if got := cs.Permit(tt.facts); got != tt.want {
				t.Errorf("Permit(%+v) = %v, want %v", tt.facts, got, tt.want)
			}
		})
	}
}

func TestScopeFilterNilPermitsAll(t *testing.T) {
	var cs *CompiledScope
	if !cs.Permit(Facts{Namespace: "anything"}) {
		t.Error("nil CompiledScope must permit everything")
	}
}

func TestScopeFilterCompileErrors(t *testing.T) {
	tests := []struct {
		name   string
		filter ScopeFilter
	}{
		{"empty allow entry", ScopeFilter{Allow: []ScopeMatch{{}}}},
		{"empty deny entry", ScopeFilter{Deny: []ScopeMatch{{}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.filter.Compile(); err == nil {
				t.Error("expected compile error, got nil")
			}
		})
	}
}
