package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/migueljfsc/wtc/internal/capture"
	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

const (
	// backfillWindow bounds the first poll of a repo with no stored
	// watermark so a fresh install doesn't ingest the repo's whole history.
	backfillWindow = 24 * time.Hour
	// overlap is subtracted from the watermark on every query so runs that
	// were still in progress when last seen get their terminal state, and
	// borderline timestamps are never skipped. Dedup makes re-ingest free.
	overlap = time.Hour
)

// Poller periodically pulls workflow runs, merged PRs, and default-branch
// commits for the configured repos and feeds them through the normalization
// pipeline. Idempotent by dedup_key, so it doubles as the webhook-loss
// sweeper and can run alongside webhooks.
type Poller struct {
	client     *Client
	store      *store.Store
	engine     *normalize.EngineHolder
	repos      []string
	interval   time.Duration
	captureDir string
	log        *slog.Logger
}

// NewPoller wires a poller; captureDir "" disables capture. The engine is a
// holder so a live rule edit re-routes poller events too (P10).
func NewPoller(client *Client, st *store.Store, engine *normalize.EngineHolder, repos []string, interval time.Duration, captureDir string, log *slog.Logger) *Poller {
	if log == nil {
		log = slog.Default()
	}
	return &Poller{
		client:     client,
		store:      st,
		engine:     engine,
		repos:      repos,
		interval:   interval,
		captureDir: captureDir,
		log:        log,
	}
}

// Run polls until ctx is cancelled. The first sweep starts immediately.
func (p *Poller) Run(ctx context.Context) {
	scope := "auto-discover (all accessible)"
	if len(p.repos) > 0 {
		scope = fmt.Sprintf("%v", p.repos)
	}
	p.log.Info("github poller starting", "repos", scope, "interval", p.interval)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		p.Sweep(ctx)
		select {
		case <-ctx.Done():
			p.log.Info("github poller stopping")
			return
		case <-ticker.C:
		}
	}
}

// Sweep polls every (repo, resource) pair once. Failures are logged and
// skipped — the watermark only advances on success, so nothing is lost. When
// no repos are configured, the accessible set is (re)discovered each sweep, so
// repos added to/removed from the token are picked up automatically.
func (p *Poller) Sweep(ctx context.Context) {
	repos := p.repos
	if len(repos) == 0 {
		discovered, err := p.client.ListAccessibleRepos(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			p.log.Error("github repo discovery failed", "error", err)
			return
		}
		p.log.Info("github poller discovered repos", "count", len(discovered))
		repos = discovered
	}
	for _, repo := range repos {
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
	watermark, err := p.store.PollWatermark(ctx, repo, resource)
	if err != nil {
		return err
	}
	if watermark.IsZero() {
		watermark = time.Now().Add(-backfillWindow)
	}
	since := watermark.Add(-overlap)

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
		if err := capture.Body(p.captureDir, "github-api", repo+"-"+resource, nil, raw); err != nil {
			p.log.Error("poller capture", "repo", repo, "resource", resource, "error", err)
		}
	}

	newest, stored, err := p.ingest(ctx, repo, resource, raw, since)
	if err != nil {
		return err
	}
	if stored > 0 {
		p.log.Info("polled", "repo", repo, "resource", resource, "stored", stored)
	}
	if newest.After(watermark) {
		return p.store.SetPollWatermark(ctx, repo, resource, newest)
	}
	return nil
}

// ingest decodes raw, normalizes every item through the shared parsers +
// rules engine, and stores them. Returns the newest source timestamp seen so
// the watermark can advance.
func (p *Poller) ingest(ctx context.Context, repo, resource string, raw []byte, since time.Time) (newest time.Time, stored int, err error) {
	var pairs []eventFacts
	switch resource {
	case "runs":
		var list restWorkflowRunList
		if err := json.Unmarshal(raw, &list); err != nil {
			return newest, 0, fmt.Errorf("decode runs: %w", err)
		}
		for _, run := range list.WorkflowRuns {
			ev, facts := NormalizeWorkflowRun(run, time.Now())
			pairs = append(pairs, eventFacts{ev, facts})
		}
	case "prs":
		var list []restPullRequest
		if err := json.Unmarshal(raw, &list); err != nil {
			return newest, 0, fmt.Errorf("decode prs: %w", err)
		}
		for _, pr := range list {
			// No server-side merged/since filter exists: drop unmerged and
			// pre-window PRs here.
			if pr.MergedAt == nil || pr.MergedAt.Before(since) {
				continue
			}
			ev, facts := NormalizeMergedPR(pr, repo, time.Now())
			if ev == nil {
				continue
			}
			// PR-diff enrichment (SPEC §7): real paths facts (env inference
			// for promotion PRs) + image-bump payload (the tag↔revision
			// link). Failure degrades to an unenriched event, never a drop.
			if enr, err := p.client.EnrichPR(ctx, repo, pr.Number); err != nil {
				p.log.Error("pr enrichment", "repo", repo, "pr", pr.Number, "error", err)
			} else {
				facts.Paths = enr.Paths
				facts.PathsTruncated = enr.PathsTruncated
				if enr.Payload != "" {
					ev.Payload = enr.Payload
				}
			}
			pairs = append(pairs, eventFacts{ev, facts})
		}
	case "commits":
		var list []restCommit
		if err := json.Unmarshal(raw, &list); err != nil {
			return newest, 0, fmt.Errorf("decode commits: %w", err)
		}
		for _, c := range list {
			ev, facts := NormalizeCommit(c, repo, time.Now())
			pairs = append(pairs, eventFacts{ev, facts})
		}
	}

	for _, pf := range pairs {
		if err := p.engine.Apply(pf.ev, pf.facts); err != nil {
			p.log.Error("rules apply", "dedup_key", pf.ev.DedupKey, "error", err)
			// Rules failing must not drop the event; it lands unenriched.
		}
		if _, _, err := p.store.Ingest(ctx, pf.ev); err != nil {
			return newest, stored, fmt.Errorf("ingest %s: %w", pf.ev.DedupKey, err)
		}
		stored++
		if pf.ev.TS.After(newest) {
			newest = pf.ev.TS
		}
	}
	return newest, stored, nil
}

type eventFacts struct {
	ev    *model.Event
	facts normalize.Facts
}
