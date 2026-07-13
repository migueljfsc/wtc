package github

import (
	"context"
	"log/slog"
	"time"

	"github.com/migueljfsc/wtc/internal/server"
	"github.com/migueljfsc/wtc/internal/store"
)

// backfillWindow bounds the first poll of a repo with no stored watermark so
// a fresh install doesn't try to ingest the repo's whole history.
const backfillWindow = 24 * time.Hour

// Poller periodically pulls workflow runs, merged PRs, and default-branch
// commits for the configured repos and feeds them through the normalization
// pipeline. Idempotent by dedup_key, so it doubles as the webhook-loss
// sweeper and can run alongside webhooks.
//
// P1 step 0: the loop, watermarking, and capture are live; normalization is
// wired in step 2 once fixtures are frozen.
type Poller struct {
	client     *Client
	store      *store.Store
	repos      []string
	interval   time.Duration
	captureDir string
	log        *slog.Logger
}

// NewPoller wires a poller; captureDir "" disables capture.
func NewPoller(client *Client, st *store.Store, repos []string, interval time.Duration, captureDir string, log *slog.Logger) *Poller {
	if log == nil {
		log = slog.Default()
	}
	return &Poller{
		client:     client,
		store:      st,
		repos:      repos,
		interval:   interval,
		captureDir: captureDir,
		log:        log,
	}
}

// Run polls until ctx is cancelled. The first sweep starts immediately.
func (p *Poller) Run(ctx context.Context) {
	p.log.Info("github poller starting", "repos", p.repos, "interval", p.interval)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		p.sweep(ctx)
		select {
		case <-ctx.Done():
			p.log.Info("github poller stopping")
			return
		case <-ticker.C:
		}
	}
}

// sweep polls every (repo, resource) pair once. Failures are logged and
// skipped — the watermark only advances on success, so nothing is lost.
func (p *Poller) sweep(ctx context.Context) {
	for _, repo := range p.repos {
		for _, res := range []string{"runs", "prs", "commits"} {
			if err := p.pollResource(ctx, repo, res); err != nil {
				if ctx.Err() != nil {
					return
				}
				p.log.Error("poll failed", "repo", repo, "resource", res, "error", err)
			}
		}
	}
}

func (p *Poller) pollResource(ctx context.Context, repo, resource string) error {
	since, err := p.store.PollWatermark(ctx, repo, resource)
	if err != nil {
		return err
	}
	if since.IsZero() {
		since = time.Now().Add(-backfillWindow)
	}

	var raw []byte
	switch resource {
	case "runs":
		raw, err = p.client.ListWorkflowRuns(ctx, repo, since)
	case "prs":
		raw, err = p.client.ListClosedPRs(ctx, repo)
	case "commits":
		raw, err = p.client.ListCommits(ctx, repo, since)
	}
	if err != nil {
		return err
	}

	if p.captureDir != "" {
		if err := server.CaptureBody(p.captureDir, "github-api", repo+"-"+resource, nil, raw); err != nil {
			p.log.Error("poller capture", "repo", repo, "resource", resource, "error", err)
		}
	}

	// Step 2 (post-fixtures): decode raw, normalize each item through the
	// shared parsers + rules engine, Ingest, then advance the watermark to
	// the newest source timestamp successfully stored.
	p.log.Debug("polled", "repo", repo, "resource", resource, "bytes", len(raw))
	return nil
}
