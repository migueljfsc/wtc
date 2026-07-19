// Package store owns database access: open/pragmas, embedded migrations, a
// single-writer goroutine consuming an ingest channel, and a read pool for
// queries. Backends: embedded SQLite (default — nothing outside `wtc serve`
// opens the DB file) and opt-in Postgres (stateless-pod deployments).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver (pure Go)
	_ "modernc.org/sqlite"             // registers the "sqlite" driver (pure Go, no CGO)

	"github.com/migueljfsc/wtc/internal/metrics"
	"github.com/migueljfsc/wtc/internal/model"
)

const busyTimeoutMS = 5000

// ErrStoreClosed is returned by Ingest once Close has begun. Callers may
// treat it as a retryable condition (the daemon is shutting down).
var ErrStoreClosed = errors.New("store is closed")

// Store is the single owner of the database.
type Store struct {
	dialect dialect
	writeDB *dbConn
	readDB  *dbConn
	writeCh chan writeReq
	wg      sync.WaitGroup

	// mu guards closed. Sends on writeCh happen under RLock; Close flips
	// closed under Lock, so no send can be in flight when writeCh closes.
	mu     sync.RWMutex
	closed bool

	upsertStmt    *sql.Stmt
	idByDedupStmt *sql.Stmt

	broadcast *broadcaster // fans newly-stored events to SSE subscribers

	// notifyFn, when set, is called from the Ingest funnel for every stored
	// NEW row and every status-changing upsert — never for a
	// rank-suppressed redelivery, which is what makes notifications naturally
	// idempotent on (event id, status). It receives the post-merge row
	// (payload/facts omitted) and must not block: the dispatcher behind it
	// owns a bounded queue. Set via SetNotifyFunc before serving starts.
	notifyFn func(ev model.Event, transitioned bool)
}

type writeReq struct {
	ev   *model.Event
	resp chan writeResp
}

type writeResp struct {
	id      string
	deduped bool
	// transitioned is true when the upsert's UPDATE arm applied to an existing
	// row. The rank guard makes that equivalent to "status changed": an update
	// runs only on a strict rank increase, and equal statuses have equal rank.
	transitioned bool
	// merged is the post-upsert row (payload/facts omitted) — identity fields
	// follow the non-empty-wins merge, so subscribers match what the ledger
	// now says, not just what the triggering delivery carried. Valid only when
	// the statement returned a row (new insert or applied update).
	merged model.Event
	err    error
}

func dsn(path string, readOnly bool) string {
	pragmas := url.Values{}
	pragmas.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMS))
	pragmas.Add("_pragma", "journal_mode(WAL)")
	pragmas.Add("_pragma", "synchronous(NORMAL)")
	if readOnly {
		pragmas.Set("mode", "ro")
	} else {
		// Must be set before the first table is created to take effect;
		// harmless no-op afterwards. Powers the retention job's
		// incremental_vacuum (later phase).
		pragmas.Add("_pragma", "auto_vacuum(INCREMENTAL)")
	}
	return "file:" + path + "?" + pragmas.Encode()
}

// upsertSQL implements the SPEC §1 rule: one row per logical change, keyed by
// dedup_key. An update applies only when the incoming status STRICTLY
// outranks the stored one (bound as ?), so a replayed or stale event of equal
// rank never regresses status or moves ts backward. Identity fields, payload,
// url, and duration_ms follow non-empty-wins merge semantics: a later event
// enriches the row but can never blank out what an earlier event recorded.
// kind and source are identity — the first event wins, never updated.
// Stored-row columns are qualified as events.<col>: postgres rejects
// unqualified names in DO UPDATE as ambiguous (42702); sqlite accepts the
// qualified form, so one statement serves both dialects.
const upsertSQL = `
INSERT INTO events (id, ts, ingested_at, source, kind, status, env, cluster,
                    namespace, service, repo, actor, ref, artifact, title, url,
                    duration_ms, dedup_key, payload, facts)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(dedup_key) DO UPDATE SET
  status      = excluded.status,
  ts          = excluded.ts,
  title       = excluded.title,
  duration_ms = coalesce(excluded.duration_ms, events.duration_ms),
  payload     = coalesce(excluded.payload, events.payload),
  facts       = coalesce(nullif(excluded.facts, ''), events.facts),
  url         = coalesce(nullif(excluded.url, ''), events.url),
  env         = coalesce(nullif(excluded.env, ''), events.env),
  cluster     = coalesce(nullif(excluded.cluster, ''), events.cluster),
  namespace   = coalesce(nullif(excluded.namespace, ''), events.namespace),
  service     = coalesce(nullif(excluded.service, ''), events.service),
  repo        = coalesce(nullif(excluded.repo, ''), events.repo),
  actor       = coalesce(nullif(excluded.actor, ''), events.actor),
  ref         = coalesce(nullif(excluded.ref, ''), events.ref),
  artifact    = coalesce(nullif(excluded.artifact, ''), events.artifact)
WHERE ? > (CASE events.status WHEN 'degraded' THEN 3 WHEN 'succeeded' THEN 2 WHEN 'failed' THEN 2 WHEN 'started' THEN 1 ELSE 0 END)
RETURNING id, ts, ingested_at, source, kind, status, env, cluster, namespace,
          service, repo, actor, ref, artifact, title, url, duration_ms, dedup_key`

// Open opens (creating if needed) the SQLite database at path, applies
// pragmas and migrations, prepares the hot-path statements, and starts the
// writer. This is the default backend — the single-binary story.
func Open(path string) (*Store, error) {
	if path == "" {
		// "file:?..." would open a SQLite private temporary database that
		// silently vanishes on close — never acceptable for a ledger.
		return nil, fmt.Errorf("store: db path must not be empty")
	}
	return open(dialectSQLite, "sqlite", dsn(path, false), dsn(path, true))
}

// OpenPostgres connects to the Postgres database at connString (the opt-in
// backend), applies migrations, and starts the writer. The write pool stays
// capped at one connection so ordering semantics are identical to sqlite.
func OpenPostgres(connString string) (*Store, error) {
	if connString == "" {
		return nil, fmt.Errorf("store: postgres dsn must not be empty")
	}
	return open(dialectPostgres, "pgx", connString, connString)
}

func open(d dialect, driver, writeDSN, readDSN string) (*Store, error) {
	rawWrite, err := sql.Open(driver, writeDSN)
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}
	// Single writer: one connection, one goroutine. On sqlite this is what
	// prevents SQLITE_BUSY fights; on postgres it keeps write ordering
	// identical so the two backends behave the same.
	rawWrite.SetMaxOpenConns(1)
	writeDB := &dbConn{DB: rawWrite, d: d}

	if err := migrate(writeDB, d); err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	upsertStmt, err := writeDB.Prepare(upsertSQL)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("prepare upsert: %w", err)
	}
	idByDedupStmt, err := writeDB.Prepare(`SELECT id FROM events WHERE dedup_key = ?`)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("prepare id lookup: %w", err)
	}

	rawRead, err := sql.Open(driver, readDSN)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open read db: %w", err)
	}
	rawRead.SetMaxOpenConns(8)

	s := &Store{
		dialect:       d,
		writeDB:       writeDB,
		readDB:        &dbConn{DB: rawRead, d: d},
		writeCh:       make(chan writeReq, 256),
		upsertStmt:    upsertStmt,
		idByDedupStmt: idByDedupStmt,
		broadcast:     newBroadcaster(),
	}
	s.wg.Add(1)
	go s.writer()
	return s, nil
}

// Close stops accepting new events, drains the writer, and closes both
// handles. Safe to call with Ingest still running concurrently: late callers
// get ErrStoreClosed instead of racing the channel close. Idempotent.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	// No sender can be mid-send here: sends hold RLock, and we held Lock
	// while flipping closed.
	close(s.writeCh)
	s.wg.Wait()

	_ = s.upsertStmt.Close()
	_ = s.idByDedupStmt.Close()
	rerr := s.readDB.Close()
	werr := s.writeDB.Close()
	if werr != nil {
		return fmt.Errorf("close write db: %w", werr)
	}
	if rerr != nil {
		return fmt.Errorf("close read db: %w", rerr)
	}
	return nil
}

// SetNotifyFunc installs the notification hook. Call before any Ingest
// runs (serve.go wires it ahead of the HTTP listener and pollers); fn must be
// non-blocking. A nil fn disables notifications.
func (s *Store) SetNotifyFunc(fn func(ev model.Event, transitioned bool)) {
	s.notifyFn = fn
}

func (s *Store) writer() {
	defer s.wg.Done()
	for req := range s.writeCh {
		req.resp <- s.upsert(req.ev)
	}
}

// Ingest stores ev (validated) through the single-writer goroutine. Returns
// the canonical row id — the original row's id when dedup_key already existed
// — and whether the event was deduplicated onto an existing row.
func (s *Store) Ingest(ctx context.Context, ev *model.Event) (id string, deduped bool, err error) {
	if err := ev.Validate(); err != nil {
		return "", false, err
	}
	req := writeReq{ev: ev, resp: make(chan writeResp, 1)}

	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return "", false, ErrStoreClosed
	}
	select {
	case s.writeCh <- req:
		s.mu.RUnlock()
	case <-ctx.Done():
		s.mu.RUnlock()
		return "", false, fmt.Errorf("ingest enqueue: %w", ctx.Err())
	}

	// The writer replies to every request it received before the channel
	// closed (resp is buffered), so this cannot leak.
	select {
	case r := <-req.resp:
		// Publish only newly-inserted rows to the live stream: re-ingested
		// duplicates (poller sweeps, redeliveries) must not flood subscribers.
		if r.err == nil && !r.deduped {
			s.broadcast.publish(*ev)
		}
		// Every ingest path (webhooks, pollers, generic) funnels through here,
		// so this is the one place the ingest/dedup counters stay complete.
		if r.err == nil {
			if r.deduped {
				metrics.Deduped.WithLabelValues(string(ev.Source)).Inc()
			} else {
				metrics.Ingested.WithLabelValues(string(ev.Source)).Inc()
			}
		}
		// Awareness hook: new rows and status transitions only — a
		// rank-suppressed redelivery (deduped, no update) never re-notifies.
		if r.err == nil && s.notifyFn != nil && (!r.deduped || r.transitioned) {
			s.notifyFn(r.merged, r.transitioned)
		}
		return r.id, r.deduped, r.err
	case <-ctx.Done():
		return "", false, fmt.Errorf("ingest wait: %w", ctx.Err())
	}
}

func (s *Store) upsert(ev *model.Event) writeResp {
	var payload, facts any
	if ev.Payload != "" {
		payload = ev.Payload
	}
	if ev.Facts != "" {
		facts = ev.Facts
	}

	var (
		merged         model.Event
		ts, ingestedAt string
		durationMS     sql.NullInt64
	)
	err := s.upsertStmt.QueryRow(
		ev.ID, model.FormatTS(ev.TS), model.FormatTS(ev.IngestedAt),
		string(ev.Source), string(ev.Kind), string(ev.Status),
		ev.Env, ev.Cluster, ev.Namespace, ev.Service, ev.Repo, ev.Actor,
		ev.Ref, ev.Artifact, ev.Title, ev.URL,
		ev.DurationMS, ev.DedupKey, payload, facts,
		model.StatusRank(ev.Status), // strict-outrank guard on the update arm
	).Scan(
		&merged.ID, &ts, &ingestedAt, &merged.Source, &merged.Kind, &merged.Status,
		&merged.Env, &merged.Cluster, &merged.Namespace, &merged.Service,
		&merged.Repo, &merged.Actor, &merged.Ref, &merged.Artifact,
		&merged.Title, &merged.URL, &durationMS, &merged.DedupKey,
	)

	switch {
	case err == nil:
		if merged.TS, err = model.ParseTS(ts); err != nil {
			return writeResp{err: err}
		}
		if merged.IngestedAt, err = model.ParseTS(ingestedAt); err != nil {
			return writeResp{err: err}
		}
		if durationMS.Valid {
			merged.DurationMS = &durationMS.Int64
		}
		deduped := merged.ID != ev.ID
		return writeResp{id: merged.ID, deduped: deduped, transitioned: deduped, merged: merged}
	case errors.Is(err, sql.ErrNoRows):
		// Conflict, and the rank guard suppressed the update: the stored row
		// is authoritative. Fetch its id to report the dedup.
		var storedID string
		if err := s.idByDedupStmt.QueryRow(ev.DedupKey).Scan(&storedID); err != nil {
			return writeResp{err: fmt.Errorf("read back event id: %w", err)}
		}
		return writeResp{id: storedID, deduped: true}
	default:
		return writeResp{err: fmt.Errorf("upsert event: %w", err)}
	}
}
