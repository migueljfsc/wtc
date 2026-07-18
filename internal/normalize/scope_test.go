package normalize

import (
	"reflect"
	"testing"
)

func TestSplitScope(t *testing.T) {
	exact, globs := SplitScope([]string{"org/api", "org/*", "org/web", "other/prefix-*"})
	if !reflect.DeepEqual(exact, []string{"org/api", "org/web"}) {
		t.Errorf("exact = %v", exact)
	}
	if len(globs) != 2 {
		t.Errorf("globs = %d, want 2", len(globs))
	}
}

func TestResolveScope(t *testing.T) {
	tests := []struct {
		name       string
		exact      []string
		patterns   []string
		candidates []string
		want       []string
	}{
		{
			name:       "org wildcard stays in one segment",
			patterns:   []string{"my-org/*"},
			candidates: []string{"my-org/api", "my-org/web", "other-org/api", "my-org/sub/deep"},
			want:       []string{"my-org/api", "my-org/web"},
		},
		{
			name:       "name prefix",
			patterns:   []string{"my-org/my-prefix-*"},
			candidates: []string{"my-org/my-prefix-api", "my-org/other", "my-org/my-prefix-web"},
			want:       []string{"my-org/my-prefix-api", "my-org/my-prefix-web"},
		},
		{
			name:       "double star crosses segments (gitlab subgroups)",
			patterns:   []string{"group/**"},
			candidates: []string{"group/app", "group/sub/app", "elsewhere/app"},
			want:       []string{"group/app", "group/sub/app"},
		},
		{
			name:       "exact unioned with matches, deduped and sorted",
			exact:      []string{"pinned/repo", "my-org/api"},
			patterns:   []string{"my-org/*"},
			candidates: []string{"my-org/api", "my-org/web"},
			want:       []string{"my-org/api", "my-org/web", "pinned/repo"},
		},
		{
			name:       "github any-org form",
			patterns:   []string{"*/deploy-*"},
			candidates: []string{"a/deploy-api", "b/deploy-web", "a/other"},
			want:       []string{"a/deploy-api", "b/deploy-web"},
		},
		{
			name:       "no candidates leaves exact only",
			exact:      []string{"pinned/repo"},
			patterns:   []string{"gone-org/*"},
			candidates: nil,
			want:       []string{"pinned/repo"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, globs := SplitScope(tt.patterns)
			got := ResolveScope(tt.exact, globs, tt.candidates)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ResolveScope = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScopeNamespace(t *testing.T) {
	tests := []struct {
		pattern string
		ns      string
		ok      bool
	}{
		{"my-group/*", "my-group", true},
		{"my-group/prefix-*", "my-group", true},
		{"group/sub/*", "group/sub", true},
		{"group/**", "group", true},
		{"*", "", false},
		{"*/x", "", false},
		{"pre*/x", "", false}, // glob inside the first segment: no static prefix
	}
	for _, tt := range tests {
		ns, ok := ScopeNamespace(tt.pattern)
		if ns != tt.ns || ok != tt.ok {
			t.Errorf("ScopeNamespace(%q) = %q,%v want %q,%v", tt.pattern, ns, ok, tt.ns, tt.ok)
		}
	}
}
