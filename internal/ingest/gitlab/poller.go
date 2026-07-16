package gitlab

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/migueljfsc/wtc/internal/capture"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

const (
	// backfillWindow bounds the first poll of a project with no stored
	// watermark so a fresh install doesn't ingest the project's whole history.
	backfillWindow = 24 * time.Hour
	// overlap is subtracted from the watermark on every query so pipelines
	// still running when last seen get their terminal state, and borderline
	// timestamps are never skipped. Dedup makes re-ingest free.
	overlap = time.Hour
)

// Poller periodically pulls pipelines, merged MRs, and default-branch commits
// for the configured projects and feeds them through the normalization
// pipeline. Idempotent by dedup_key, so it doubles as the webhook-loss sweeper
// and can run alongside webhooks. Mirrors internal/ingest/github.Poller.
type Poller struct {
	client     *Client
	store      *store.Store
	engine     *normalize.EngineHolder
	projects   []string
	interval   time.Duration
	captureDir string
	log        *slog.Logger
}

// NewPoller wires a poller; captureDir "" disables capture. The engine is a
// holder so a live rule edit re-routes poller events too (P10).
func NewPoller(client *Client, st *store.Store, engine *normalize.EngineHolder, projects []string, interval time.Duration, captureDir string, log *slog.Logger) *Poller {
	if log == nil {
		log = slog.Default()
	}
	return &Poller{
		client:     client,
		store:      st,
		engine:     engine,
		projects:   projects,
		interval:   interval,
		captureDir: captureDir,
		log:        log,
	}
}

// Run polls until ctx is cancelled. The first sweep starts immediately.
func (p *Poller) Run(ctx context.Context) {
	p.log.Info("gitlab poller starting", "projects", p.projects, "interval", p.interval)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		p.Sweep(ctx)
		select {
		case <-ctx.Done():
			p.log.Info("gitlab poller stopping")
			return
		case <-ticker.C:
		}
	}
}

// Sweep polls every (project, resource) pair once. Failures are logged and
// skipped — the watermark only advances on success, so nothing is lost. Unlike
// GitHub there is no cheap "all accessible projects" discovery equivalent that
// maps to the poller model, so projects must be configured explicitly.
func (p *Poller) Sweep(ctx context.Context) {
	for _, project := range p.projects {
		for _, res := range []string{"pipelines", "mrs", "commits"} {
			if err := p.pollResource(ctx, project, res); err != nil {
				if ctx.Err() != nil {
					return
				}
				p.log.Error("poll failed", "project", project, "resource", res, "error", err)
			}
		}
	}
}

func (p *Poller) pollResource(ctx context.Context, project, resource string) error {
	watermark, err := p.store.PollWatermark(ctx, project, "gitlab:"+resource)
	if err != nil {
		return err
	}
	if watermark.IsZero() {
		watermark = time.Now().Add(-backfillWindow)
	}
	since := watermark.Add(-overlap)

	var raw []byte
	switch resource {
	case "pipelines":
		raw, err = p.client.ListPipelines(ctx, project, since)
	case "mrs":
		raw, err = p.client.ListMergedMRs(ctx, project, since)
	case "commits":
		raw, err = p.client.ListCommits(ctx, project, since)
	}
	if err != nil {
		return err
	}

	if p.captureDir != "" {
		if err := capture.Body(p.captureDir, "gitlab-api", project+"-"+resource, nil, raw); err != nil {
			p.log.Error("poller capture", "project", project, "resource", resource, "error", err)
		}
	}

	newest, stored, err := p.ingest(ctx, project, resource, raw, since)
	if err != nil {
		return err
	}
	if stored > 0 {
		p.log.Info("polled", "project", project, "resource", resource, "stored", stored)
	}
	if newest.After(watermark) {
		return p.store.SetPollWatermark(ctx, project, "gitlab:"+resource, newest)
	}
	return nil
}

// ingest decodes raw, normalizes every item through the shared parsers + rules
// engine, and stores them. Returns the newest source timestamp seen so the
// watermark can advance.
func (p *Poller) ingest(ctx context.Context, project, resource string, raw []byte, since time.Time) (newest time.Time, stored int, err error) {
	var pairs []EventFacts
	switch resource {
	case "pipelines":
		// The list is sparse; fetch per-pipeline detail for the rich event.
		var list []restPipeline
		if err := decodeInto(raw, &list); err != nil {
			return newest, 0, fmt.Errorf("decode pipelines: %w", err)
		}
		for _, item := range list {
			// The list item lacks finished_at/duration/actor; fetch detail.
			// On any detail failure fall back to the sparse list item — a
			// path-unknown event beats a dropped one.
			pl := item
			if detailRaw, err := p.client.GetPipeline(ctx, project, item.ID); err != nil {
				p.log.Error("pipeline detail", "project", project, "pipeline", item.ID, "error", err)
			} else if err := decodeInto(detailRaw, &pl); err != nil {
				p.log.Error("decode pipeline detail", "project", project, "pipeline", item.ID, "error", err)
				pl = item
			}
			ev, facts := NormalizePipeline(pl, project, time.Now())
			pairs = append(pairs, EventFacts{ev, facts})
		}
	case "mrs":
		var list []restMergeRequest
		if err := decodeInto(raw, &list); err != nil {
			return newest, 0, fmt.Errorf("decode merge requests: %w", err)
		}
		for _, mr := range list {
			ev, facts := NormalizeMergedMR(mr, project, time.Now())
			if ev == nil {
				continue
			}
			// MR-diff enrichment (SPEC §7): real paths (env inference for
			// promotion MRs) + image-bump payload (the tag↔revision link).
			// Failure degrades to an unenriched event, never a drop.
			if enr, err := p.client.EnrichMR(ctx, project, mr.IID); err != nil {
				p.log.Error("mr enrichment", "project", project, "mr", mr.IID, "error", err)
			} else {
				facts.Paths = enr.Paths
				facts.PathsTruncated = enr.PathsTruncated
				if enr.Payload != "" {
					ev.Payload = enr.Payload
				}
			}
			pairs = append(pairs, EventFacts{ev, facts})
		}
	case "commits":
		var list []restCommit
		if err := decodeInto(raw, &list); err != nil {
			return newest, 0, fmt.Errorf("decode commits: %w", err)
		}
		for _, c := range list {
			ev, facts := NormalizeCommit(c, project, time.Now())
			pairs = append(pairs, EventFacts{ev, facts})
		}
	}

	for _, pf := range pairs {
		if err := p.engine.Apply(pf.Event, pf.Facts); err != nil {
			p.log.Error("rules apply", "dedup_key", pf.Event.DedupKey, "error", err)
			// Rules failing must not drop the event; it lands unenriched.
		}
		if _, _, err := p.store.Ingest(ctx, pf.Event); err != nil {
			return newest, stored, fmt.Errorf("ingest %s: %w", pf.Event.DedupKey, err)
		}
		stored++
		if pf.Event.TS.After(newest) {
			newest = pf.Event.TS
		}
	}
	return newest, stored, nil
}
