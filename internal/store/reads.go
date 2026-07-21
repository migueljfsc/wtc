package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// Targeted read queries backing wtc where / diff / handoff (internal/query).

func kindPlaceholders(kinds []model.Kind) (string, []any) {
	ph := make([]string, len(kinds))
	args := make([]any, len(kinds))
	for i, k := range kinds {
		ph[i] = "?"
		args[i] = string(k)
	}
	return strings.Join(ph, ","), args
}

func (s *Store) queryEvents(ctx context.Context, q string, args ...any) ([]model.Event, error) {
	rows, err := s.readDB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	// Non-nil so callers that marshal these lists emit [] — the openapi
	// contract types them as required arrays, and clients index them.
	events := []model.Event{}
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	return events, nil
}

// EventsByRefPrefix returns events of the given kinds whose ref starts with
// prefix (a full or short git sha), oldest first.
func (s *Store) EventsByRefPrefix(ctx context.Context, prefix string, kinds []model.Kind) ([]model.Event, error) {
	ph, args := kindPlaceholders(kinds)
	args = append(args, escapeLike(prefix)+"%")
	return s.queryEvents(ctx,
		`SELECT `+eventColumns+` FROM events
		 WHERE kind IN (`+ph+`) AND ref LIKE ? ESCAPE '\'
		 ORDER BY ts ASC`, args...)
}

// EventsPayloadContaining returns events of the given kinds whose payload
// contains needle (cheap pre-filter; callers verify precisely), oldest first.
func (s *Store) EventsPayloadContaining(ctx context.Context, needle string, kinds []model.Kind) ([]model.Event, error) {
	ph, args := kindPlaceholders(kinds)
	args = append(args, "%"+escapeLike(needle)+"%")
	return s.queryEvents(ctx,
		`SELECT `+eventColumns+` FROM events
		 WHERE kind IN (`+ph+`) AND payload LIKE ? ESCAPE '\'
		 ORDER BY ts ASC`, args...)
}

// EventsArtifactContaining is EventsPayloadContaining over the artifact column.
func (s *Store) EventsArtifactContaining(ctx context.Context, needle string, kinds []model.Kind) ([]model.Event, error) {
	ph, args := kindPlaceholders(kinds)
	args = append(args, "%"+escapeLike(needle)+"%")
	return s.queryEvents(ctx,
		`SELECT `+eventColumns+` FROM events
		 WHERE kind IN (`+ph+`) AND artifact LIKE ? ESCAPE '\'
		 ORDER BY ts ASC`, args...)
}

// LatestSucceededDeploys returns, for each (env, service) pair in envs, the
// most recent successful deploy. Deploys without a service are skipped —
// diff is per-service by definition. A non-zero asOf bounds the search to
// deploys at or before that instant, reconstructing the state that was
// running then; the zero value means "now" (no upper bound).
func (s *Store) LatestSucceededDeploys(ctx context.Context, envs []string, asOf time.Time) ([]model.Event, error) {
	ph := make([]string, len(envs))
	args := make([]any, len(envs))
	for i, e := range envs {
		ph[i] = "?"
		args[i] = e
	}
	q := `SELECT ` + eventColumns + ` FROM events
		 WHERE kind = 'deploy' AND status = 'succeeded'
		   AND env IN (` + strings.Join(ph, ",") + `) AND service != ''`
	if !asOf.IsZero() {
		q += ` AND ts <= ?`
		args = append(args, model.FormatTS(asOf))
	}
	q += ` ORDER BY ts DESC`
	all, err := s.queryEvents(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	seen := map[[2]string]bool{}
	latest := []model.Event{}
	for _, ev := range all {
		key := [2]string{ev.Env, ev.Service}
		if seen[key] {
			continue
		}
		seen[key] = true
		latest = append(latest, ev)
	}
	return latest, nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// EventByID fetches one event; sql.ErrNoRows when absent.
func (s *Store) EventByID(ctx context.Context, id string) (*model.Event, error) {
	events, err := s.queryEvents(ctx,
		`SELECT `+eventColumns+` FROM events WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, sql.ErrNoRows
	}
	return &events[0], nil
}

// ServicesFirstSeenSince lists services whose earliest event ever falls
// inside the window — genuinely new services, not just recently active ones.
func (s *Store) ServicesFirstSeenSince(ctx context.Context, since time.Time) ([]string, error) {
	rows, err := s.readDB.QueryContext(ctx, `
SELECT service FROM events
WHERE service != ''
GROUP BY service
HAVING MIN(ts) >= ?
ORDER BY service`, model.FormatTS(since))
	if err != nil {
		return nil, fmt.Errorf("first-seen services: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var svc string
		if err := rows.Scan(&svc); err != nil {
			return nil, err
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}
