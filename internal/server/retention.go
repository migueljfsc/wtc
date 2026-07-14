package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/migueljfsc/wtc/internal/store"
)

const (
	defaultRetentionInterval      = 24 * time.Hour
	defaultRetentionEphemeralGlob = "pr-*"
)

// RetentionScheduler prunes events past their retention cutoff on a fixed
// interval. Simple ticker like DigestScheduler — a low-traffic self-hosted
// tool needs no calendar scheduling.
type RetentionScheduler struct {
	store            *store.Store
	keep             time.Duration
	ephemeralKeep    time.Duration
	ephemeralPattern string
	interval         time.Duration
	log              *slog.Logger
}

// NewRetentionScheduler returns nil (disabled) unless keep is positive.
// Interval defaults to 24h and the ephemeral pattern to "pr-*" when unset.
func NewRetentionScheduler(st *store.Store, keep, ephemeralKeep, interval time.Duration, ephemeralPattern string, log *slog.Logger) *RetentionScheduler {
	if keep <= 0 {
		return nil
	}
	if interval <= 0 {
		interval = defaultRetentionInterval
	}
	if ephemeralPattern == "" {
		ephemeralPattern = defaultRetentionEphemeralGlob
	}
	if log == nil {
		log = slog.Default()
	}
	return &RetentionScheduler{
		store:            st,
		keep:             keep,
		ephemeralKeep:    ephemeralKeep,
		ephemeralPattern: ephemeralPattern,
		interval:         interval,
		log:              log,
	}
}

// Run prunes once on start (cleans stale rows at boot; idempotent, so a box
// that restarts often still gets pruned) then every interval until ctx is
// cancelled.
func (r *RetentionScheduler) Run(ctx context.Context) {
	r.log.Info("retention scheduler started",
		"keep", r.keep, "ephemeral_keep", r.ephemeralKeep,
		"ephemeral_pattern", r.ephemeralPattern, "interval", r.interval)
	r.prune(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.prune(ctx)
		}
	}
}

func (r *RetentionScheduler) prune(ctx context.Context) {
	res, err := r.store.Retain(ctx, time.Now(), r.keep, r.ephemeralKeep, r.ephemeralPattern)
	if err != nil {
		r.log.Error("retention: prune", "error", err)
		return
	}
	if res.DeletedNormal > 0 || res.DeletedEphemeral > 0 {
		r.log.Info("retention: pruned",
			"normal", res.DeletedNormal, "ephemeral", res.DeletedEphemeral)
	}
}
