// Package store owns SQLite access: open/pragmas, embedded migrations, a
// single-writer goroutine consuming an ingest channel, and a read-only pool
// for queries. Nothing outside `wtc serve` opens the DB file.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sync"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go, no CGO)

	"github.com/migueljfsc/wtc/internal/model"
)

const busyTimeoutMS = 5000

// ErrStoreClosed is returned by Ingest once Close has begun. Callers may
// treat it as a retryable condition (the daemon is shutting down).
var ErrStoreClosed = errors.New("store is closed")

// Store is the single owner of the SQLite database.
type Store struct {
	writeDB *sql.DB
	readDB  *sql.DB
	writeCh chan writeReq
	wg      sync.WaitGroup

	// mu guards closed. Sends on writeCh happen under RLock; Close flips
	// closed under Lock, so no send can be in flight when writeCh closes.
	mu     sync.RWMutex
	closed bool

	upsertStmt    *sql.Stmt
	idByDedupStmt *sql.Stmt

	broadcast *broadcaster // fans newly-stored events to SSE subscribers
}

type writeReq struct {
	ev   *model.Event
	resp chan writeResp
}

type writeResp struct {
	id      string
	deduped bool
	err     error
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
const upsertSQL = `
INSERT INTO events (id, ts, ingested_at, source, kind, status, env, cluster,
                    namespace, service, actor, ref, artifact, title, url,
                    duration_ms, dedup_key, payload)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(dedup_key) DO UPDATE SET
  status      = excluded.status,
  ts          = excluded.ts,
  title       = excluded.title,
  duration_ms = coalesce(excluded.duration_ms, duration_ms),
  payload     = coalesce(excluded.payload, payload),
  url         = coalesce(nullif(excluded.url, ''), url),
  env         = coalesce(nullif(excluded.env, ''), env),
  cluster     = coalesce(nullif(excluded.cluster, ''), cluster),
  namespace   = coalesce(nullif(excluded.namespace, ''), namespace),
  service     = coalesce(nullif(excluded.service, ''), service),
  actor       = coalesce(nullif(excluded.actor, ''), actor),
  ref         = coalesce(nullif(excluded.ref, ''), ref),
  artifact    = coalesce(nullif(excluded.artifact, ''), artifact)
WHERE ? > (CASE events.status WHEN 'degraded' THEN 3 WHEN 'succeeded' THEN 2 WHEN 'failed' THEN 2 WHEN 'started' THEN 1 ELSE 0 END)
RETURNING id`

// Open opens (creating if needed) the database at path, applies pragmas and
// migrations, prepares the hot-path statements, and starts the writer.
func Open(path string) (*Store, error) {
	if path == "" {
		// "file:?..." would open a SQLite private temporary database that
		// silently vanishes on close — never acceptable for a ledger.
		return nil, fmt.Errorf("store: db path must not be empty")
	}

	writeDB, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}
	// Single writer: one connection, one goroutine, zero SQLITE_BUSY fights.
	writeDB.SetMaxOpenConns(1)

	if err := migrate(writeDB); err != nil {
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

	readDB, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open read db: %w", err)
	}
	readDB.SetMaxOpenConns(8)

	s := &Store{
		writeDB:       writeDB,
		readDB:        readDB,
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

func (s *Store) writer() {
	defer s.wg.Done()
	for req := range s.writeCh {
		id, deduped, err := s.upsert(req.ev)
		req.resp <- writeResp{id: id, deduped: deduped, err: err}
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
		return r.id, r.deduped, r.err
	case <-ctx.Done():
		return "", false, fmt.Errorf("ingest wait: %w", ctx.Err())
	}
}

func (s *Store) upsert(ev *model.Event) (string, bool, error) {
	var payload any
	if ev.Payload != "" {
		payload = ev.Payload
	}

	var storedID string
	err := s.upsertStmt.QueryRow(
		ev.ID, model.FormatTS(ev.TS), model.FormatTS(ev.IngestedAt),
		string(ev.Source), string(ev.Kind), string(ev.Status),
		ev.Env, ev.Cluster, ev.Namespace, ev.Service, ev.Actor,
		ev.Ref, ev.Artifact, ev.Title, ev.URL,
		ev.DurationMS, ev.DedupKey, payload,
		model.StatusRank(ev.Status), // strict-outrank guard on the update arm
	).Scan(&storedID)

	switch {
	case err == nil:
		return storedID, storedID != ev.ID, nil
	case errors.Is(err, sql.ErrNoRows):
		// Conflict, and the rank guard suppressed the update: the stored row
		// is authoritative. Fetch its id to report the dedup.
		if err := s.idByDedupStmt.QueryRow(ev.DedupKey).Scan(&storedID); err != nil {
			return "", false, fmt.Errorf("read back event id: %w", err)
		}
		return storedID, true, nil
	default:
		return "", false, fmt.Errorf("upsert event: %w", err)
	}
}
