// Package github implements the GitHub ingest paths: the API poller (primary
// for private deployments) and, together with internal/server, the webhook
// normalizers. Parsers are written against captured fixtures only —
// see CLAUDE.md fixture-first workflow.
package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const apiBase = "https://api.github.com"

// Client is a minimal hand-rolled GitHub REST client. Three endpoints, one
// token — a full SDK dependency isn't justified (CLAUDE.md: minimal deps).
type Client struct {
	base  string
	token string
	http  *http.Client
}

// NewClient builds a client; base overrides the API root for tests ("" =
// api.github.com).
func NewClient(token, base string) *Client {
	if base == "" {
		base = apiBase
	}
	return &Client{
		base:  base,
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Get performs one authenticated GET and returns the raw body. Callers
// decode; capture mode wants the untouched bytes.
func (c *Client) Get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	u := c.base + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github api %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("github api %s: read body: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api %s: HTTP %d: %.200s", path, resp.StatusCode, body)
	}
	return body, nil
}

// The three poller resources (SPEC §7).

// ListWorkflowRuns returns raw JSON of workflow runs created since the given
// time (GitHub supports created>= filtering server-side).
func (c *Client) ListWorkflowRuns(ctx context.Context, repo string, since time.Time) ([]byte, error) {
	params := url.Values{
		"per_page": {"100"},
		"created":  {">=" + since.UTC().Format("2006-01-02T15:04:05Z")},
	}
	return c.Get(ctx, "/repos/"+repo+"/actions/runs", params)
}

// ListClosedPRs returns raw JSON of recently updated closed PRs; the poller
// filters merged_at > watermark client-side (no server-side merged filter).
func (c *Client) ListClosedPRs(ctx context.Context, repo string) ([]byte, error) {
	params := url.Values{
		"state":     {"closed"},
		"sort":      {"updated"},
		"direction": {"desc"},
		"per_page":  {"50"},
	}
	return c.Get(ctx, "/repos/"+repo+"/pulls", params)
}

// ListCommits returns raw JSON of default-branch commits since the given time.
func (c *Client) ListCommits(ctx context.Context, repo string, since time.Time) ([]byte, error) {
	params := url.Values{
		"per_page": {"100"},
		"since":    {since.UTC().Format(time.RFC3339)},
	}
	return c.Get(ctx, "/repos/"+repo+"/commits", params)
}
