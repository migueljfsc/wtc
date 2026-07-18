package gitlab

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestParseProjectPathsFixture pins the namespace-listing parser to the
// captured gitlab.com payload (fixture-first).
func TestParseProjectPathsFixture(t *testing.T) {
	body := readFixture(t, apiDir, "namespace_projects.json")
	paths, total, err := parseProjectPaths(body)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || !reflect.DeepEqual(paths, []string{"migueljfsc/wtc-demo-gitlab"}) {
		t.Fatalf("paths = %v (total %d), want [migueljfsc/wtc-demo-gitlab] (1)", paths, total)
	}
}

// TestListNamespaceProjectsUserFallback: user namespaces are not groups —
// GitLab 404s the group endpoint and the client must fall back to the user
// endpoint (verified live against gitlab.com: /groups/migueljfsc → 404,
// /users/migueljfsc/projects → the rig project).
func TestListNamespaceProjectsUserFallback(t *testing.T) {
	listing := readFixture(t, apiDir, "namespace_projects.json")
	var groupHit, userHit bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/groups/migueljfsc/projects", func(w http.ResponseWriter, _ *http.Request) {
		groupHit = true
		http.Error(w, `{"message":"404 Group Not Found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("GET /api/v4/users/migueljfsc/projects", func(w http.ResponseWriter, _ *http.Request) {
		userHit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(listing)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	paths, err := NewClient("t", srv.URL).ListNamespaceProjects(t.Context(), "migueljfsc")
	if err != nil {
		t.Fatal(err)
	}
	if !groupHit || !userHit {
		t.Fatalf("fallback not exercised: group=%v user=%v", groupHit, userHit)
	}
	if !reflect.DeepEqual(paths, []string{"migueljfsc/wtc-demo-gitlab"}) {
		t.Fatalf("paths = %v", paths)
	}
}

// TestListNamespaceProjectsGroup: a real group answers on the first endpoint —
// no fallback call.
func TestListNamespaceProjectsGroup(t *testing.T) {
	listing := readFixture(t, apiDir, "namespace_projects.json")
	var userHit bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/groups/my-group/projects", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(listing)
	})
	mux.HandleFunc("GET /api/v4/users/", func(w http.ResponseWriter, _ *http.Request) {
		userHit = true
		http.Error(w, "unexpected", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	paths, err := NewClient("t", srv.URL).ListNamespaceProjects(t.Context(), "my-group")
	if err != nil {
		t.Fatal(err)
	}
	if userHit {
		t.Fatal("user endpoint hit although the group answered")
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %v", paths)
	}
}
