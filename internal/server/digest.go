package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/migueljfsc/wtc/internal/notify"
	"github.com/migueljfsc/wtc/internal/query"
	"github.com/migueljfsc/wtc/internal/store"
)

// DigestScheduler posts a Slack handoff digest on a fixed interval. Simple
// ticker, not cron — a low-traffic self-hosted tool doesn't need calendar
// scheduling, and it keeps the dependency list minimal.
type DigestScheduler struct {
	store    *store.Store
	webhook  string
	interval time.Duration
	window   time.Duration
	log      *slog.Logger
}

// NewDigestScheduler returns nil (disabled) unless both webhook and a
// positive interval are configured.
func NewDigestScheduler(st *store.Store, webhook string, interval, window time.Duration, log *slog.Logger) *DigestScheduler {
	if webhook == "" || interval <= 0 {
		return nil
	}
	if window <= 0 {
		window = interval
	}
	if log == nil {
		log = slog.Default()
	}
	return &DigestScheduler{store: st, webhook: webhook, interval: interval, window: window, log: log}
}

// Run posts a digest every interval until ctx is cancelled. It does NOT fire
// immediately on start (avoids a digest on every restart); the first one
// lands one interval in.
func (d *DigestScheduler) Run(ctx context.Context) {
	d.log.Info("digest scheduler started", "interval", d.interval, "window", d.window)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.post(ctx)
		}
	}
}

func (d *DigestScheduler) post(ctx context.Context) {
	report, err := query.Handoff(ctx, d.store, time.Now().Add(-d.window))
	if err != nil {
		d.log.Error("digest: build report", "error", err)
		return
	}
	if err := notify.Slack(ctx, d.webhook, report.SlackText(time.Now())); err != nil {
		d.log.Error("digest: post to slack", "error", err)
		return
	}
	d.log.Info("digest posted to slack")
}
