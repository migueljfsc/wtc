package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

// ErrInvalidCursor marks a malformed pagination cursor — client input error,
// not a storage failure. Handlers map it to 400.
var ErrInvalidCursor = errors.New("invalid cursor")

// Filter selects events for ListEvents. Zero values mean "no constraint".
type Filter struct {
	Env     string
	Service string
	Kind    string
	Status  string
	Query   string // FTS5 MATCH over title/service/actor/artifact
	Since   time.Time
	Until   time.Time
	Limit   int
	Cursor  string // opaque, from a previous ListEvents call
}

// ListEvents returns events newest-first (ts DESC, id DESC) with cursor
// pagination. nextCursor is "" when there are no further pages.
func (s *Store) ListEvents(ctx context.Context, f Filter) (events []model.Event, nextCursor string, err error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	var conds []string
	var args []any
	add := func(cond string, vals ...any) {
		conds = append(conds, cond)
		args = append(args, vals...)
	}

	if f.Env != "" {
		add("env = ?", f.Env)
	}
	if f.Service != "" {
		add("service = ?", f.Service)
	}
	if f.Kind != "" {
		add("kind = ?", f.Kind)
	}
	if f.Status != "" {
		add("status = ?", f.Status)
	}
	if !f.Since.IsZero() {
		add("ts >= ?", model.FormatTS(f.Since))
	}
	if !f.Until.IsZero() {
		add("ts <= ?", model.FormatTS(f.Until))
	}
	if f.Query != "" {
		add("rowid IN (SELECT rowid FROM events_fts WHERE events_fts MATCH ?)", ftsQuery(f.Query))
	}
	if f.Cursor != "" {
		ts, id, err := decodeCursor(f.Cursor)
		if err != nil {
			return nil, "", err
		}
		add("(ts < ? OR (ts = ? AND id < ?))", ts, ts, id)
	}

	q := `SELECT id, ts, ingested_at, source, kind, status, env, cluster,
	             namespace, service, actor, ref, artifact, title, url,
	             duration_ms, dedup_key, payload
	      FROM events`
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY ts DESC, id DESC LIMIT ?"
	args = append(args, limit+1) // one extra row to detect a next page

	rows, err := s.readDB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	events = make([]model.Event, 0, limit)
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, "", err
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("list events: %w", err)
	}

	if len(events) > limit {
		events = events[:limit]
		last := events[len(events)-1]
		nextCursor = encodeCursor(model.FormatTS(last.TS), last.ID)
	}
	return events, nextCursor, nil
}

func scanEvent(rows *sql.Rows) (model.Event, error) {
	var (
		ev             model.Event
		ts, ingestedAt string
		durationMS     sql.NullInt64
		payload        sql.NullString
	)
	if err := rows.Scan(
		&ev.ID, &ts, &ingestedAt, &ev.Source, &ev.Kind, &ev.Status,
		&ev.Env, &ev.Cluster, &ev.Namespace, &ev.Service, &ev.Actor,
		&ev.Ref, &ev.Artifact, &ev.Title, &ev.URL,
		&durationMS, &ev.DedupKey, &payload,
	); err != nil {
		return model.Event{}, fmt.Errorf("scan event: %w", err)
	}

	var err error
	if ev.TS, err = model.ParseTS(ts); err != nil {
		return model.Event{}, err
	}
	if ev.IngestedAt, err = model.ParseTS(ingestedAt); err != nil {
		return model.Event{}, err
	}
	if durationMS.Valid {
		ev.DurationMS = &durationMS.Int64
	}
	ev.Payload = payload.String
	return ev, nil
}

// ftsQuery turns free text into a safe FTS5 prefix query: each term is
// double-quoted (so FTS5 operators/punctuation in user input can't inject
// syntax errors) with a trailing * for prefix matching.
func ftsQuery(q string) string {
	terms := strings.Fields(q)
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"*`)
	}
	return strings.Join(quoted, " ")
}

// Cursor format: base64url("<stored ts>\x00<id>"). Opaque to clients.
func encodeCursor(ts, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(ts + "\x00" + id))
}

func decodeCursor(c string) (ts, id string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	ts, id, ok := strings.Cut(string(raw), "\x00")
	if !ok || ts == "" || id == "" {
		return "", "", ErrInvalidCursor
	}
	return ts, id, nil
}
