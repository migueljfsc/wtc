package github

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Golden shape test: the real captured /pulls/{n}/files response decodes and
// yields paths (motorcycle-journey PR #1: two workflow files, no yaml bumps).
func TestGoldenPRFilesShape(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(fixtureDir, "pull_request_files.json"))
	if err != nil {
		t.Fatal(err)
	}
	var files []prFile
	if err := json.Unmarshal(raw, &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	for _, f := range files {
		if f.Filename == "" || f.Status == "" || f.Patch == "" {
			t.Errorf("file missing fields: %+v", f)
		}
		if bumps := extractBumps(f); bumps != nil {
			t.Errorf("workflow file yielded bumps: %+v", bumps)
		}
	}
}

// Bump extraction over a realistic kustomize promotion patch. Patch content
// synthetic (fixture-first covers the API shape above; this pins the regex
// semantics until a real promotion PR is captured from the demo stack).
func TestExtractBumps(t *testing.T) {
	tests := []struct {
		name string
		file prFile
		want []ImageBump
	}{
		{
			name: "kustomize newTag bump",
			file: prFile{
				Filename: "demo/api/infrastructure/overlays/prod/kustomization.yaml",
				Patch: `@@ -5,7 +5,7 @@ resources:
   - ../../base
 images:
   - name: ghcr.io/migueljfsc/wtc-demo-api
-    newTag: sha-abc1234
+    newTag: sha-def5678`,
			},
			want: []ImageBump{{
				File: "demo/api/infrastructure/overlays/prod/kustomization.yaml",
				Old:  "sha-abc1234",
				New:  "sha-def5678",
			}},
		},
		{
			name: "helm values tag with quotes",
			file: prFile{
				Filename: "infrastructure/overlays/staging/values.yaml",
				Patch: `@@ -1,4 +1,4 @@
 image:
   repository: reg/app
-  tag: "1.4.1-aaa1111"
+  tag: "1.4.2-bbb2222"`,
			},
			want: []ImageBump{{
				File: "infrastructure/overlays/staging/values.yaml",
				Old:  "1.4.1-aaa1111",
				New:  "1.4.2-bbb2222",
			}},
		},
		{
			name: "addition without removal (first pin)",
			file: prFile{
				Filename: "x/kustomization.yaml",
				Patch:    "@@ -0,0 +1,2 @@\n+images:\n+    newTag: sha-abc1234",
			},
			want: []ImageBump{{File: "x/kustomization.yaml", New: "sha-abc1234"}},
		},
		{
			name: "non-yaml ignored",
			file: prFile{Filename: "main.go", Patch: "+\tnewTag: sha-abc1234"},
			want: nil,
		},
		{
			name: "hunk header not misparsed",
			file: prFile{
				Filename: "y.yaml",
				Patch:    "@@ -1,3 +1,3 @@\n--- old\n+++ new\n context",
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBumps(tt.file)
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("bump %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
