package normalize

import "testing"

func TestTagResolverDefaults(t *testing.T) {
	r, err := NewTagResolver(nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		tag  string
		want string
	}{
		{"sha-abc1234", "abc1234"},
		{"sha-46b93c870014ed9ee43488f0463b0f0744ac327c", "46b93c870014ed9ee43488f0463b0f0744ac327c"},
		{"1.4.2-abc1234", "abc1234"},
		{"v1.4.2-abc1234", "abc1234"},
		{"0.2.0-b759b17", "b759b17"},
		{"ghcr.io/migueljfsc/wtc-demo-api:sha-b759b17", "b759b17"}, // full image ref
		{"ghcr.io/x/y:0.2.0-b759b17", "b759b17"},
		{"latest", ""},
		{"1.4.2", ""},        // semver without sha
		{"sha-xyz9999", ""},  // not hex
		{"main-abc1234", ""}, // convention not configured
		{"", ""},
	}
	for _, tt := range tests {
		if got := r.Resolve(tt.tag); got != tt.want {
			t.Errorf("Resolve(%q) = %q, want %q", tt.tag, got, tt.want)
		}
	}
}

func TestTagResolverCustomAndErrors(t *testing.T) {
	r, err := NewTagResolver([]string{`^main-(?P<sha>[0-9a-f]{7,40})-\d+$`})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Resolve("main-abc1234-1720000000"); got != "abc1234" {
		t.Errorf("custom pattern: got %q", got)
	}
	if got := r.Resolve("sha-abc1234"); got != "" {
		t.Errorf("defaults must be replaced, not appended: got %q", got)
	}

	if _, err := NewTagResolver([]string{`^broken(`}); err == nil {
		t.Error("invalid regex must error")
	}
	if _, err := NewTagResolver([]string{`^no-group$`}); err == nil {
		t.Error("missing sha group must error")
	}
}
