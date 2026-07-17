package store

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateResult reports what a ledger migration copied.
type MigrateResult struct {
	Events        int64 `json:"events"`
	EventsSkipped int64 `json:"events_skipped"` // already present (re-run)
	Watermarks    int64 `json:"watermarks"`
	Overrides     int64 `json:"overrides"`
}

// MigrateLedger copies a sqlite ledger into a postgres database (P15 one-shot
// switch-over): events, poller watermarks, and DB-backed config overrides.
// The postgres schema is created (migrations run) if absent. Idempotent — all
// inserts are ON CONFLICT DO NOTHING, so an interrupted run can simply be
// re-run. The sqlite file is opened read-only; run with `wtc serve` stopped so
// the copy is a consistent final snapshot.
//
// This is the one deliberate exception to "the CLI never opens the DB file":
// an offline admin operation on a stopped ledger, exactly like serve itself.
func MigrateLedger(ctx context.Context, sqlitePath, pgDSN string) (*MigrateResult, error) {
	src, err := sql.Open("sqlite", dsn(sqlitePath, true))
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", sqlitePath, err)
	}
	defer func() { _ = src.Close() }()
	// Fail fast on a missing/empty file rather than at first row.
	var n int64
	if err := src.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
		return nil, fmt.Errorf("read sqlite events (is %s a wtc ledger?): %w", sqlitePath, err)
	}

	dst, err := OpenPostgres(pgDSN) // runs postgres migrations
	if err != nil {
		return nil, err
	}
	defer func() { _ = dst.Close() }()

	res := &MigrateResult{}
	if err := copyEvents(ctx, src, dst.writeDB, res); err != nil {
		return nil, err
	}
	if err := copyTable(ctx, src, dst.writeDB,
		`SELECT repo, resource, watermark, updated_at FROM github_poll_state`,
		`INSERT INTO github_poll_state (repo, resource, watermark, updated_at)
		 VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`, 4, &res.Watermarks); err != nil {
		return nil, err
	}
	if err := copyTable(ctx, src, dst.writeDB,
		`SELECT key, value, updated_at FROM config_overrides`,
		`INSERT INTO config_overrides (key, value, updated_at)
		 VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, 3, &res.Overrides); err != nil {
		return nil, err
	}
	return res, nil
}

func copyEvents(ctx context.Context, src *sql.DB, dst *dbConn, res *MigrateResult) error {
	rows, err := src.QueryContext(ctx, `SELECT `+eventColumns+` FROM events ORDER BY ts`)
	if err != nil {
		return fmt.Errorf("read sqlite events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tx, err := dst.Begin()
	if err != nil {
		return fmt.Errorf("begin postgres tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ins, err := tx.Prepare(dst.rebind(`
INSERT INTO events (id, ts, ingested_at, source, kind, status, env, cluster,
                    namespace, service, actor, ref, artifact, title, url,
                    duration_ms, dedup_key, payload)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`))
	if err != nil {
		return fmt.Errorf("prepare postgres insert: %w", err)
	}
	defer func() { _ = ins.Close() }()

	for rows.Next() {
		// Scan raw column values — no Event round-trip, so rows that predate
		// today's validation rules still copy verbatim.
		var (
			id, ts, ingestedAt, source, kind, status string
			env, cluster, namespace, service, actor  string
			ref, artifact, title, url, dedupKey      string
			durationMS                               sql.NullInt64
			payload                                  sql.NullString
		)
		if err := rows.Scan(&id, &ts, &ingestedAt, &source, &kind, &status,
			&env, &cluster, &namespace, &service, &actor,
			&ref, &artifact, &title, &url, &durationMS, &dedupKey, &payload); err != nil {
			return fmt.Errorf("scan sqlite event: %w", err)
		}
		r, err := ins.ExecContext(ctx, id, ts, ingestedAt, source, kind, status,
			env, cluster, namespace, service, actor,
			ref, artifact, title, url, durationMS, dedupKey, payload)
		if err != nil {
			return fmt.Errorf("insert event %s: %w", id, err)
		}
		if inserted, _ := r.RowsAffected(); inserted == 1 {
			res.Events++
		} else {
			res.EventsSkipped++
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read sqlite events: %w", err)
	}
	return tx.Commit()
}

// copyTable streams a small table (watermarks, overrides) row by row. cols is
// the column count of both statements.
func copyTable(ctx context.Context, src *sql.DB, dst *dbConn, selectSQL, insertSQL string, cols int, copied *int64) error {
	rows, err := src.QueryContext(ctx, selectSQL)
	if err != nil {
		return fmt.Errorf("read sqlite table: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		vals := make([]any, cols)
		ptrs := make([]any, cols)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}
		r, err := dst.ExecContext(ctx, insertSQL, vals...)
		if err != nil {
			return fmt.Errorf("insert row: %w", err)
		}
		if inserted, _ := r.RowsAffected(); inserted == 1 {
			*copied++
		}
	}
	return rows.Err()
}
