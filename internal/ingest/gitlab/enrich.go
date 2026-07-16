package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// MR-diff enrichment (SPEC §7 analog): the changed-file list of a merged MR
// gives (a) real paths facts — path rules can infer env for promotion MRs —
// and (b) image-tag bumps extracted from diff lines, creating the tag↔manifest-
// revision link `wtc where` traverses. Only matched lines are stored, never
// diff bodies. Mirrors internal/ingest/github.EnrichPR.

// mrChanges is GET /projects/:id/merge_requests/:iid/changes — shape verified
// against testdata/gitlab/api/merge_request_changes.json.
type mrChanges struct {
	Changes []struct {
		NewPath string `json:"new_path"`
		OldPath string `json:"old_path"`
		Diff    string `json:"diff"` // unified diff for this file, no +++/--- headers
	} `json:"changes"`
	Overflow bool `json:"overflow"` // GitLab truncated the change set
}

// Default bump-extraction regexes (SPEC §7): kustomize newTag and generic yaml
// tag keys. Applied line-wise to diff hunks — same patterns as the GitHub
// enricher; kept per-package so ingest packages stay independent.
var bumpPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s*newTag:\s*["']?(\S+?)["']?\s*$`),
	regexp.MustCompile(`^\s*tag:\s*["']?(\S+?)["']?\s*$`),
}

// ImageBump is one extracted tag change, stored in the merge event's payload.
type ImageBump struct {
	File string `json:"file"`
	Old  string `json:"old,omitempty"`
	New  string `json:"new"`
}

// Enrichment is the result attached to a merged-MR event.
type Enrichment struct {
	Paths          []string
	PathsTruncated bool
	Payload        string // JSON {"image_bumps": [...]} or "" when no bumps
}

// EnrichMR fetches and digests a merged MR's changed files.
func (c *Client) EnrichMR(ctx context.Context, project string, iid int) (*Enrichment, error) {
	raw, err := c.GetMRChanges(ctx, project, iid)
	if err != nil {
		return nil, err
	}
	var mc mrChanges
	if err := json.Unmarshal(raw, &mc); err != nil {
		return nil, fmt.Errorf("decode mr changes: %w", err)
	}
	e := &Enrichment{PathsTruncated: mc.Overflow}
	var bumps []ImageBump
	for _, ch := range mc.Changes {
		e.Paths = append(e.Paths, ch.NewPath)
		bumps = append(bumps, extractBumps(ch.NewPath, ch.Diff)...)
	}
	if len(bumps) > 0 {
		payload, err := json.Marshal(map[string]any{"image_bumps": bumps})
		if err != nil {
			return nil, fmt.Errorf("encode bumps: %w", err)
		}
		e.Payload = string(payload)
	}
	return e, nil
}

// extractBumps scans a yaml file's unified diff for tag additions/removals.
// Only the matched values survive; the rest of the diff is discarded.
func extractBumps(filename, diff string) []ImageBump {
	if diff == "" || !isYAML(filename) {
		return nil
	}
	var olds, news []string
	for _, line := range strings.Split(diff, "\n") {
		if len(line) < 2 || (line[0] != '+' && line[0] != '-') || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		for _, re := range bumpPatterns {
			if m := re.FindStringSubmatch(line[1:]); m != nil {
				if line[0] == '+' {
					news = append(news, m[1])
				} else {
					olds = append(olds, m[1])
				}
				break
			}
		}
	}
	var bumps []ImageBump
	for i, n := range news {
		b := ImageBump{File: filename, New: n}
		if i < len(olds) {
			b.Old = olds[i]
		}
		bumps = append(bumps, b)
	}
	return bumps
}

func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}
