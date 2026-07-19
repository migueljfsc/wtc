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

// WebhookChurn flags a likely-unstable mapping-webhook dedup_key: rows
// sharing (source, title, kind, status) that landed inside a tight window but
// under DISTINCT dedup_keys — the signature of a sender retrying with a key
// that varies per delivery (e.g. embeds a timestamp). Legitimate distinct
// changes do not cluster this tightly. Heuristic, meant to be eyeballed.
type WebhookChurn struct {
	Source  string `json:"source"`
	Title   string `json:"title"`
	Rows    int    `json:"rows"`     // distinct dedup_keys in the cluster
	WindowS int    `json:"window_s"` // seconds the cluster spans
}

// WebhookMappingError is a per-source count of recent mapping-template eval
// failures. Populated by the server (in-memory), merged into
// the doctor report — a mapping error must surface, never be guessed at.
type WebhookMappingError struct {
	Source    string    `json:"source"`
	Count     int       `json:"count"`
	LastError string    `json:"last_error"`
	LastAt    time.Time `json:"last_at"`
}

// DoctorReport is the /api/doctor payload (SPEC §6). OldestEvent lets an
// operator eyeball retention (how far back the ledger reaches vs. keep); a
// dedup-drop counter is still not tracked.
type DoctorReport struct {
	TotalEvents          int64                 `json:"total_events"`
	DBSizeBytes          int64                 `json:"db_size_bytes"`
	OldestEvent          *time.Time            `json:"oldest_event,omitempty"` // nil on an empty ledger
	Sources              []SourceHealth        `json:"sources"`
	Unmapped24h          int                   `json:"unmapped_24h"`
	UnmappedSamples      []string              `json:"unmapped_samples,omitempty"`
	ClockSkew24h         int                   `json:"clock_skew_24h"`
	Poll                 []PollState           `json:"poll,omitempty"`
	WebhookChurn         []WebhookChurn        `json:"webhook_churn,omitempty"`          // unstable dedup_key heuristic
	WebhookMappingErrors []WebhookMappingError `json:"webhook_mapping_errors,omitempty"` // recent template eval failures
}

// SizeBytes returns the database size in bytes (per-dialect query). Shared by
// the doctor report and the wtc_db_size_bytes metric collector.
func (s *Store) SizeBytes(ctx context.Context) (int64, error) {
	sizeSQL := `SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`
	if s.dialect == dialectPostgres {
		sizeSQL = `SELECT pg_database_size(current_database())`
	}
	var n int64
	if err := s.readDB.QueryRowContext(ctx, sizeSQL).Scan(&n); err != nil {
		return 0, fmt.Errorf("db size: %w", err)
	}
	return n, nil
}

// Backend returns the storage backend name ("sqlite" or "postgres") — metric
// label and diagnostics only.
func (s *Store) Backend() string {
	if s.dialect == dialectPostgres {
		return "postgres"
	}
	return "sqlite"
}

// Doctor gathers source health from the read pool (poll state from writeDB,
// where that table lives — a single cheap row scan).
func (s *Store) Doctor(ctx context.Context, now time.Time) (*DoctorReport, error) {
	r := &DoctorReport{Sources: []SourceHealth{}} // sources: [] on an empty ledger, never null
	dayAgo := model.FormatTS(now.Add(-24 * time.Hour))

	if err := s.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&r.TotalEvents); err != nil {
		return nil, fmt.Errorf("doctor: total: %w", err)
	}
	size, err := s.SizeBytes(ctx)
	if err != nil {
		return nil, fmt.Errorf("doctor: %w", err)
	}
	r.DBSizeBytes = size

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

	// Unmapped: env inference failed — surfaced, never guessed.
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

	// |ts - ingested_at| > 10m flags out-of-order arrival / clock skew.
	skewSQL := `
SELECT COUNT(*) FROM events
WHERE ts >= ? AND ABS(julianday(ts) - julianday(ingested_at)) > 10.0/1440.0`
	if s.dialect == dialectPostgres {
		skewSQL = `
SELECT COUNT(*) FROM events
WHERE ts >= ? AND ABS(EXTRACT(EPOCH FROM (ts::timestamptz - ingested_at::timestamptz))) > 600`
	}
	if err := s.readDB.QueryRowContext(ctx, skewSQL, dayAgo).Scan(&r.ClockSkew24h); err != nil {
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

	if err := s.webhookChurn(ctx, r, dayAgo); err != nil {
		return nil, err
	}

	return r, nil
}

// churn heuristic thresholds: a cluster of >= churnMinRows rows sharing
// (source,title,kind,status) but with distinct dedup_keys, spanning less than
// churnWindow, is flagged as a probable unstable dedup_key.
const (
	churnMinRows = 5
	churnWindow  = 5 * time.Minute
)

// webhookChurn detects likely-unstable mapping-webhook dedup keys. It groups
// 24h events by (source,title,kind,status) and flags groups whose distinct
// dedup_keys number >= churnMinRows within a churnWindow span — rows that
// SHOULD have collapsed onto one row but did not. Runs over all sources (the
// signal is meaningful for any parser), but the footgun it targets is the
// operator-authored mapping-webhook dedup_key template.
func (s *Store) webhookChurn(ctx context.Context, r *DoctorReport, dayAgo string) error {
	churnSQL := `
SELECT source, title, COUNT(DISTINCT dedup_key) AS keys,
       CAST((julianday(MAX(ts)) - julianday(MIN(ts))) * 86400 AS INTEGER) AS span_s
FROM events
WHERE ts >= ?
GROUP BY source, title, kind, status
HAVING keys >= ? AND (julianday(MAX(ts)) - julianday(MIN(ts))) <= ?
ORDER BY keys DESC
LIMIT 10`
	var windowArg any = churnWindow.Minutes() / 1440.0 // julianday units: days
	if s.dialect == dialectPostgres {
		// Postgres can't reference a select alias in HAVING — repeat the
		// aggregates; the window compares in seconds via EXTRACT(EPOCH).
		churnSQL = `
SELECT source, title, COUNT(DISTINCT dedup_key) AS keys,
       EXTRACT(EPOCH FROM (MAX(ts)::timestamptz - MIN(ts)::timestamptz))::int AS span_s
FROM events
WHERE ts >= ?
GROUP BY source, title, kind, status
HAVING COUNT(DISTINCT dedup_key) >= ?
   AND EXTRACT(EPOCH FROM (MAX(ts)::timestamptz - MIN(ts)::timestamptz)) <= ?
ORDER BY keys DESC
LIMIT 10`
		windowArg = churnWindow.Seconds()
	}
	rows, err := s.readDB.QueryContext(ctx, churnSQL, dayAgo, churnMinRows, windowArg)
	if err != nil {
		return fmt.Errorf("doctor: webhook churn: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var c WebhookChurn
		if err := rows.Scan(&c.Source, &c.Title, &c.Rows, &c.WindowS); err != nil {
			return fmt.Errorf("doctor: scan churn: %w", err)
		}
		r.WebhookChurn = append(r.WebhookChurn, c)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("doctor: webhook churn: %w", err)
	}
	return nil
}
