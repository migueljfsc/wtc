package catalog

import "testing"

func fullCatalog(t *testing.T, sources []Source) *Catalog {
	t.Helper()
	c, err := Load(sources)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

var allSources = []Source{
	{Type: "backstage", Path: "testdata/backstage/catalog-info.yaml"},
	{Type: "datadog", Path: "testdata/service.datadog.yaml"},
	{Type: "services", Path: "testdata/services.yaml"},
	{Type: "codeowners", Path: "testdata/CODEOWNERS", Repo: "acme/legacy"},
}

func TestCatalogFormatsAndPriority(t *testing.T) {
	c := fullCatalog(t, allSources)

	cases := []struct {
		name          string
		service, repo string
		want          string
	}{
		{"backstage wins contested api (group: stripped)", "api", "", "platform"},
		{"backstage web", "web", "", "frontend"},
		{"datadog worker (services.yaml lower priority ignored)", "worker", "", "data"},
		{"services.yaml catalog", "catalog", "", "commerce"},
		{"non-Component entity ignored", "storefront", "", ""},
		{"codeowners repo fallback", "", "acme/legacy", "acme/legacy-team"},
		{"services.yaml repo mapping", "", "acme/storefront-catalog", "commerce"},
		{"service match beats repo fallback", "api", "acme/legacy", "platform"},
		{"unknown service and repo", "nope", "nope", ""},
	}
	for _, tc := range cases {
		if got := c.Owner(tc.service, tc.repo); got != tc.want {
			t.Errorf("%s: Owner(%q,%q) = %q, want %q", tc.name, tc.service, tc.repo, got, tc.want)
		}
	}
}

// Priority follows source type, not config listing order: reversing the list
// still lets backstage win the contested `api`.
func TestCatalogPriorityIsTypeOrdered(t *testing.T) {
	reversed := make([]Source, len(allSources))
	for i, s := range allSources {
		reversed[len(allSources)-1-i] = s
	}
	c := fullCatalog(t, reversed)
	if got := c.Owner("api", ""); got != "platform" {
		t.Errorf("api owner with reversed sources = %q, want platform (backstage priority)", got)
	}
}

func TestCatalogNilSafe(t *testing.T) {
	var c *Catalog
	if got := c.Owner("api", "acme/legacy"); got != "" {
		t.Errorf("nil catalog Owner = %q, want empty", got)
	}
	if c.Len() != 0 {
		t.Errorf("nil catalog Len = %d, want 0", c.Len())
	}
}

func TestCatalogLoadErrors(t *testing.T) {
	cases := []struct {
		name    string
		sources []Source
	}{
		{"unknown type", []Source{{Type: "nope", Path: "testdata/services.yaml"}}},
		{"missing file", []Source{{Type: "services", Path: "testdata/does-not-exist.yaml"}}},
		{"glob matches nothing", []Source{{Type: "services", Path: "testdata/*.nope"}}},
		{"codeowners without repo", []Source{{Type: "codeowners", Path: "testdata/CODEOWNERS"}}},
	}
	for _, tc := range cases {
		if _, err := Load(tc.sources); err == nil {
			t.Errorf("%s: Load succeeded, want error", tc.name)
		}
	}
}
