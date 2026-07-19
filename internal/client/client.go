// Package client is the thin HTTP client every CLI subcommand (except serve)
// uses to talk to the serve API. The CLI never opens the DB file directly.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/ingest/generic"
	"github.com/migueljfsc/wtc/internal/query"
	"github.com/migueljfsc/wtc/internal/server"
	"github.com/migueljfsc/wtc/internal/store"
)

// Client talks to a running `wtc serve`.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New builds a client for base (e.g. http://localhost:8484).
func New(base, token string) *Client {
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("wtc server unreachable at %s: %w", c.base, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		var apiErr server.ErrorResponse
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error != "" {
			return fmt.Errorf("server: %s (HTTP %d)", apiErr.Error, resp.StatusCode)
		}
		return fmt.Errorf("server: HTTP %d", resp.StatusCode)
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// IngestGeneric posts one event to /ingest/generic.
func (c *Client) IngestGeneric(ctx context.Context, req generic.Request) (server.IngestResponse, error) {
	var out server.IngestResponse
	err := c.do(ctx, http.MethodPost, "/ingest/generic", req, &out)
	return out, err
}

// Events queries /api/events with the given query parameters.
func (c *Client) Events(ctx context.Context, params url.Values) (server.EventsResponse, error) {
	var out server.EventsResponse
	path := "/api/events"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// Doctor fetches the source-health report.
func (c *Client) Doctor(ctx context.Context) (store.DoctorReport, error) {
	var out store.DoctorReport
	err := c.do(ctx, http.MethodGet, "/api/doctor", nil, &out)
	return out, err
}

// Config fetches the effective config: redacted static snapshot plus
// the live normalization parts. Secrets arrive masked — the server never
// sends values.
func (c *Client) Config(ctx context.Context) (server.ConfigResponse, error) {
	var out server.ConfigResponse
	err := c.do(ctx, http.MethodGet, "/api/config", nil, &out)
	return out, err
}

// Where fetches a change's BUILD → INTENT → APPLIED journey.
func (c *Client) Where(ctx context.Context, ref string) (query.WhereReport, error) {
	var out query.WhereReport
	err := c.do(ctx, http.MethodGet, "/api/where/"+url.PathEscape(ref), nil, &out)
	return out, err
}

// Diff compares two environments.
func (c *Client) Diff(ctx context.Context, a, b string) (query.DiffReport, error) {
	var out query.DiffReport
	params := url.Values{"a": {a}, "b": {b}}
	err := c.do(ctx, http.MethodGet, "/api/diff?"+params.Encode(), nil, &out)
	return out, err
}

// Around fetches the changes in a window before an instant (ts) or before
// an event (id) — typically an alert.
func (c *Client) Around(ctx context.Context, params url.Values) (server.EventsResponse, error) {
	var out server.EventsResponse
	err := c.do(ctx, http.MethodGet, "/api/around?"+params.Encode(), nil, &out)
	return out, err
}

// Blast fetches the ranked suspect changes for an alert anchor (id or ts) —
// or, anchored on a change, the alerts that fired after it.
func (c *Client) Blast(ctx context.Context, params url.Values) (query.BlastReport, error) {
	var out query.BlastReport
	err := c.do(ctx, http.MethodGet, "/api/blast?"+params.Encode(), nil, &out)
	return out, err
}

// Explain fetches the per-field inference trace for an event.
func (c *Client) Explain(ctx context.Context, id string) (server.ExplainReport, error) {
	var out server.ExplainReport
	err := c.do(ctx, http.MethodGet, "/api/explain/"+url.PathEscape(id), nil, &out)
	return out, err
}

// Download streams a raw GET endpoint (export, backup) into w and returns
// the bytes written. Unlike do() it uses a client without an overall timeout
// — a large ledger streams for as long as it needs; cancel via ctx.
func (c *Client) Download(ctx context.Context, path string, w io.Writer) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("wtc server unreachable at %s: %w", c.base, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		var apiErr server.ErrorResponse
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error != "" {
			return 0, fmt.Errorf("server: %s (HTTP %d)", apiErr.Error, resp.StatusCode)
		}
		return 0, fmt.Errorf("server: HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(w, resp.Body)
	if err != nil {
		return n, fmt.Errorf("stream response: %w", err)
	}
	return n, nil
}

// Handoff fetches the activity digest since the given instant.
func (c *Client) Handoff(ctx context.Context, since string) (query.HandoffReport, error) {
	var out query.HandoffReport
	params := url.Values{"since": {since}}
	err := c.do(ctx, http.MethodGet, "/api/handoff?"+params.Encode(), nil, &out)
	return out, err
}
