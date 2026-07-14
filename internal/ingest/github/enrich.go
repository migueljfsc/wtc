package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// PR-diff enrichment (SPEC §7): the changed-file list of a merged PR gives
// (a) real paths facts — path rules can infer env for promotion PRs — and
// (b) image-tag bumps extracted from patch lines, creating the
// tag↔manifest-revision link `wtc where` traverses. Only matched lines are
// stored, never diff bodies.

// prFile is one item of GET /repos/{repo}/pulls/{n}/files — shape verified
// against testdata/github/rest/pull_request_files.json.
type prFile struct {
	Filename string `json:"filename"`
	Status   string `json:"status"`
	Patch    string `json:"patch"` // absent for binary/huge files
}

const prFilesPerPage = 100

// ListPRFiles fetches one page of a merged PR's changed files. truncated
// reports a full page — callers must treat the path list as incomplete
// (trap #3), exactly like GitHub's own push-payload cap.
func (c *Client) ListPRFiles(ctx context.Context, repo string, number int) (files []prFile, truncated bool, err error) {
	raw, err := c.Get(ctx, fmt.Sprintf("/repos/%s/pulls/%d/files", repo, number), url.Values{
		"per_page": {fmt.Sprint(prFilesPerPage)},
	})
	if err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(raw, &files); err != nil {
		return nil, false, fmt.Errorf("decode pr files: %w", err)
	}
	return files, len(files) >= prFilesPerPage, nil
}

// Default bump-extraction regexes (SPEC §7): kustomize newTag and generic
// yaml tag keys. Applied line-wise to patch hunks.
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

// extractBumps scans a yaml file's patch for tag additions/removals. Only
// the matched values survive; the rest of the diff is discarded.
func extractBumps(f prFile) []ImageBump {
	if f.Patch == "" || !isYAML(f.Filename) {
		return nil
	}
	var olds, news []string
	for _, line := range strings.Split(f.Patch, "\n") {
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
		b := ImageBump{File: f.Filename, New: n}
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

// Enrichment is the result attached to a merged-PR event.
type Enrichment struct {
	Paths          []string
	PathsTruncated bool
	Payload        string // JSON {"image_bumps": [...]} or "" when no bumps
}

// EnrichPR fetches and digests a merged PR's changed files.
func (c *Client) EnrichPR(ctx context.Context, repo string, number int) (*Enrichment, error) {
	files, truncated, err := c.ListPRFiles(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	e := &Enrichment{PathsTruncated: truncated}
	var bumps []ImageBump
	for _, f := range files {
		e.Paths = append(e.Paths, f.Filename)
		bumps = append(bumps, extractBumps(f)...)
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
