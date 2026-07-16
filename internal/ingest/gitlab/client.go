package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBase = "https://gitlab.com"

// Client is a minimal hand-rolled GitLab REST (v4) client. A handful of
// endpoints, one token — a full SDK dependency isn't justified (CLAUDE.md:
// minimal deps). Mirrors internal/ingest/github.Client.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// NewClient builds a client; base overrides the instance root for
// self-managed GitLab and tests ("" = gitlab.com). The token is sent as
// PRIVATE-TOKEN (personal/project/group access token).
func NewClient(token, base string) *Client {
	if base == "" {
		base = defaultBase
	}
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

// encodeProject URL-encodes a project path ("group/service") for use as a
// path segment — GitLab addresses projects by id or by the "/"→"%2F" encoded
// path. Project paths contain no other reserved characters.
func encodeProject(path string) string {
	return strings.ReplaceAll(path, "/", "%2F")
}

// get performs one authenticated GET and returns the raw body. Callers decode;
// capture mode wants the untouched bytes.
func (c *Client) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u := c.base + "/api/v4" + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("PRIVATE-TOKEN", c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab api %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("gitlab api %s: read body: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab api %s: HTTP %d: %.200s", path, resp.StatusCode, body)
	}
	return body, nil
}

// The three poller resources (SPEC §7 analog).

// ListPipelines returns raw JSON of pipelines updated at/after since, oldest
// first (so the watermark advances monotonically). The list items are sparse
// — the poller fetches per-pipeline detail for finished_at/duration/actor.
func (c *Client) ListPipelines(ctx context.Context, project string, since time.Time) ([]byte, error) {
	return c.get(ctx, "/projects/"+encodeProject(project)+"/pipelines", url.Values{
		"updated_after": {since.UTC().Format(time.RFC3339)},
		"order_by":      {"updated_at"},
		"sort":          {"asc"},
		"per_page":      {"100"},
	})
}

// GetPipeline returns raw JSON of a single pipeline (the rich object:
// finished_at, duration, user).
func (c *Client) GetPipeline(ctx context.Context, project string, id int64) ([]byte, error) {
	return c.get(ctx, fmt.Sprintf("/projects/%s/pipelines/%d", encodeProject(project), id), nil)
}

// ListMergedMRs returns raw JSON of merged MRs updated at/after since. GitLab
// supports state=merged server-side (unlike GitHub's closed-then-filter).
func (c *Client) ListMergedMRs(ctx context.Context, project string, since time.Time) ([]byte, error) {
	return c.get(ctx, "/projects/"+encodeProject(project)+"/merge_requests", url.Values{
		"state":         {"merged"},
		"updated_after": {since.UTC().Format(time.RFC3339)},
		"order_by":      {"updated_at"},
		"sort":          {"asc"},
		"per_page":      {"100"},
	})
}

// ListCommits returns raw JSON of default-branch commits since the given time
// (no ref_name ⇒ the project's default branch).
func (c *Client) ListCommits(ctx context.Context, project string, since time.Time) ([]byte, error) {
	return c.get(ctx, "/projects/"+encodeProject(project)+"/repository/commits", url.Values{
		"since":    {since.UTC().Format(time.RFC3339)},
		"per_page": {"100"},
	})
}

// GetMRChanges returns raw JSON of an MR's changed files with diffs (the
// MR-diff enrichment source: real paths + image-tag bumps).
func (c *Client) GetMRChanges(ctx context.Context, project string, iid int) ([]byte, error) {
	return c.get(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d/changes", encodeProject(project), iid), nil)
}

// decodeInto is a small helper for the poller: decode a raw body into v.
func decodeInto(raw []byte, v any) error {
	return json.Unmarshal(raw, v)
}
