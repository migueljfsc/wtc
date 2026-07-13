package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// Poll-state reads/writes go straight to writeDB rather than through the
// ingest channel: they are rare (once per poll cycle per resource), carry no
// ordering relationship with events, and MaxOpenConns(1) already serializes
// them against the writer goroutine.

// PollWatermark returns the stored high-water mark for (repo, resource), or
// the zero time when none exists yet (first run → caller applies its
// bounded-backfill default).
func (s *Store) PollWatermark(ctx context.Context, repo, resource string) (time.Time, error) {
	var ts string
	err := s.writeDB.QueryRowContext(ctx,
		`SELECT watermark FROM github_poll_state WHERE repo = ? AND resource = ?`,
		repo, resource,
	).Scan(&ts)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return time.Time{}, nil
	case err != nil:
		return time.Time{}, fmt.Errorf("read poll watermark %s/%s: %w", repo, resource, err)
	}
	return model.ParseTS(ts)
}

// SetPollWatermark persists the newest source timestamp seen for
// (repo, resource). Monotonic: an older value than the stored one is ignored.
func (s *Store) SetPollWatermark(ctx context.Context, repo, resource string, watermark time.Time) error {
	_, err := s.writeDB.ExecContext(ctx, `
INSERT INTO github_poll_state (repo, resource, watermark, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(repo, resource) DO UPDATE SET
  watermark  = excluded.watermark,
  updated_at = excluded.updated_at
WHERE excluded.watermark > watermark`,
		repo, resource, model.FormatTS(watermark), model.FormatTS(time.Now()),
	)
	if err != nil {
		return fmt.Errorf("set poll watermark %s/%s: %w", repo, resource, err)
	}
	return nil
}
