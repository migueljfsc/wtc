package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// SourceHealth summarizes one ingest source's recent activity.
type SourceHealth struct {
	Source   string    `json:"source"`
	Count24h int       `json:"count_24h"`
	LastTS   time.Time `json:"last_ts"`
}

// PollState is one poller high-water mark row.
type PollState struct {
	Repo      string    `json:"repo"`
	Resource  string    `json:"resource"`
	Watermark time.Time `json:"watermark"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DoctorReport is the /api/doctor payload (SPEC §6). OldestEvent lets an
// operator eyeball retention (how far back the ledger reaches vs. keep); a
// dedup-drop counter is still not tracked.
type DoctorReport struct {
	TotalEvents     int64          `json:"total_events"`
	DBSizeBytes     int64          `json:"db_size_bytes"`
	OldestEvent     *time.Time     `json:"oldest_event,omitempty"` // nil on an empty ledger
	Sources         []SourceHealth `json:"sources"`
	Unmapped24h     int            `json:"unmapped_24h"`
	UnmappedSamples []string       `json:"unmapped_samples,omitempty"`
	ClockSkew24h    int            `json:"clock_skew_24h"`
	Poll            []PollState    `json:"poll,omitempty"`
}

// Doctor gathers source health from the read pool (poll state from writeDB,
// where that table lives — a single cheap row scan).
func (s *Store) Doctor(ctx context.Context, now time.Time) (*DoctorReport, error) {
	r := &DoctorReport{}
	dayAgo := model.FormatTS(now.Add(-24 * time.Hour))

	if err := s.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&r.TotalEvents); err != nil {
		return nil, fmt.Errorf("doctor: total: %w", err)
	}
	if err := s.readDB.QueryRowContext(ctx,
		`SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`,
	).Scan(&r.DBSizeBytes); err != nil {
		return nil, fmt.Errorf("doctor: db size: %w", err)
	}

	// Oldest retained event (NULL on an empty ledger) — a quick retention gauge.
	var oldest sql.NullString
	if err := s.readDB.QueryRowContext(ctx, `SELECT MIN(ts) FROM events`).Scan(&oldest); err != nil {
		return nil, fmt.Errorf("doctor: oldest: %w", err)
	}
	if oldest.Valid {
		t, err := model.ParseTS(oldest.String)
		if err != nil {
			return nil, err
		}
		r.OldestEvent = &t
	}

	rows, err := s.readDB.QueryContext(ctx, `
SELECT source,
       SUM(CASE WHEN ts >= ? THEN 1 ELSE 0 END),
       MAX(ts)
FROM events GROUP BY source ORDER BY source`, dayAgo)
	if err != nil {
		return nil, fmt.Errorf("doctor: sources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var sh SourceHealth
		var last string
		if err := rows.Scan(&sh.Source, &sh.Count24h, &last); err != nil {
			return nil, fmt.Errorf("doctor: scan source: %w", err)
		}
		if sh.LastTS, err = model.ParseTS(last); err != nil {
			return nil, err
		}
		r.Sources = append(r.Sources, sh)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("doctor: sources: %w", err)
	}

	// Unmapped: env inference failed (trap #2) — surfaced, never guessed.
	if err := s.readDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE env = '' AND ts >= ?`, dayAgo,
	).Scan(&r.Unmapped24h); err != nil {
		return nil, fmt.Errorf("doctor: unmapped: %w", err)
	}
	if r.Unmapped24h > 0 {
		samples, err := s.readDB.QueryContext(ctx,
			`SELECT title FROM events WHERE env = '' AND ts >= ? ORDER BY ts DESC LIMIT 3`, dayAgo)
		if err != nil {
			return nil, fmt.Errorf("doctor: samples: %w", err)
		}
		defer func() { _ = samples.Close() }()
		for samples.Next() {
			var t string
			if err := samples.Scan(&t); err != nil {
				return nil, err
			}
			r.UnmappedSamples = append(r.UnmappedSamples, t)
		}
		if err := samples.Err(); err != nil {
			return nil, err
		}
	}

	// |ts - ingested_at| > 10m flags out-of-order arrival / clock skew (trap #6).
	if err := s.readDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM events
WHERE ts >= ? AND ABS(julianday(ts) - julianday(ingested_at)) > 10.0/1440.0`, dayAgo,
	).Scan(&r.ClockSkew24h); err != nil {
		return nil, fmt.Errorf("doctor: clock skew: %w", err)
	}

	poll, err := s.writeDB.QueryContext(ctx,
		`SELECT repo, resource, watermark, updated_at FROM github_poll_state ORDER BY repo, resource`)
	if err != nil {
		return nil, fmt.Errorf("doctor: poll state: %w", err)
	}
	defer func() { _ = poll.Close() }()
	for poll.Next() {
		var ps PollState
		var wm, up string
		if err := poll.Scan(&ps.Repo, &ps.Resource, &wm, &up); err != nil {
			return nil, fmt.Errorf("doctor: scan poll state: %w", err)
		}
		if ps.Watermark, err = model.ParseTS(wm); err != nil {
			return nil, err
		}
		if ps.UpdatedAt, err = model.ParseTS(up); err != nil {
			return nil, err
		}
		r.Poll = append(r.Poll, ps)
	}
	if err := poll.Err(); err != nil {
		return nil, fmt.Errorf("doctor: poll state: %w", err)
	}

	return r, nil
}
