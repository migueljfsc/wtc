package gitlab

import (
	"encoding/json"
	"testing"
)

// Golden: the MR-changes fixture yields the real changed path plus the
// kustomize newTag bump (sha-0000000 → sha-190b65d7), the tag↔revision link
// `wtc where` traverses.
func TestGoldenExtractBumps(t *testing.T) {
	var mc mrChanges
	if err := json.Unmarshal(readFixture(t, apiDir, "merge_request_changes.json"), &mc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(mc.Changes) == 0 {
		t.Fatal("no changes in fixture")
	}
	var bumps []ImageBump
	var paths []string
	for _, ch := range mc.Changes {
		paths = append(paths, ch.NewPath)
		bumps = append(bumps, extractBumps(ch.NewPath, ch.Diff)...)
	}
	if len(paths) != 1 || paths[0] != "infrastructure/overlays/dev/kustomization.yaml" {
		t.Errorf("paths = %v", paths)
	}
	if len(bumps) != 1 {
		t.Fatalf("bumps = %d, want 1", len(bumps))
	}
	b := bumps[0]
	if b.New != "sha-190b65d7" || b.Old != "sha-0000000" {
		t.Errorf("bump = %+v, want new=sha-190b65d7 old=sha-0000000", b)
	}
	if b.File != "infrastructure/overlays/dev/kustomization.yaml" {
		t.Errorf("bump file = %q", b.File)
	}
}

// Enriching a merged MR with the fixture paths makes the dev-overlay path rule
// fire: the merge Event resolves to env=dev (the positive env-inference case,
// paired with the root-only push that stays "").
func TestMergeEnvInferenceFromEnrichedPaths(t *testing.T) {
	var list []restMergeRequest
	decodeAPI(t, "merge_requests_merged.json", &list)
	ev, facts := NormalizeMergedMR(list[0], testProject, testNow)
	if ev == nil {
		t.Fatal("nil event")
	}
	// Simulate enrichment: attach the real changed paths.
	facts.Paths = []string{"infrastructure/overlays/dev/kustomization.yaml"}
	facts.PathsTruncated = false

	if err := devEnvEngine(t).Apply(ev, facts); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if ev.Env != "dev" {
		t.Errorf("env = %q, want dev", ev.Env)
	}
}

// Non-yaml or empty diffs produce no bumps.
func TestExtractBumpsIgnoresNonYAML(t *testing.T) {
	if b := extractBumps("main.go", "+\tnewTag: nope\n"); b != nil {
		t.Errorf("non-yaml yielded bumps: %+v", b)
	}
	if b := extractBumps("k.yaml", ""); b != nil {
		t.Errorf("empty diff yielded bumps: %+v", b)
	}
}
