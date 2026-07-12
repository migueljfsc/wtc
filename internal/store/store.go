// Package store owns SQLite access: open/pragmas, embedded migrations, a
// single-writer goroutine consuming an ingest channel, and a read-only pool
// for queries. Nothing outside `wtc serve` opens the DB file.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sync"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go, no CGO)

	"github.com/migueljfsc/wtc/internal/model"
)

const busyTimeoutMS = 5000

// Store is the single owner of the SQLite database.
type Store struct {
	writeDB *sql.DB
	readDB  *sql.DB
	writeCh chan writeReq
	wg      sync.WaitGroup
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

// Open opens (creating if needed) the database at path, applies pragmas and
// migrations, and starts the writer goroutine.
func Open(path string) (*Store, error) {
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

	readDB, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open read db: %w", err)
	}
	readDB.SetMaxOpenConns(8)

	s := &Store{
		writeDB: writeDB,
		readDB:  readDB,
		writeCh: make(chan writeReq, 256),
	}
	s.wg.Add(1)
	go s.writer()
	return s, nil
}

// Close drains the writer and closes both handles. Callers must stop
// producing Ingest calls first (shut the HTTP server down before the store).
func (s *Store) Close() error {
	close(s.writeCh)
	s.wg.Wait()
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
	select {
	case s.writeCh <- req:
	case <-ctx.Done():
		return "", false, fmt.Errorf("ingest enqueue: %w", ctx.Err())
	}
	select {
	case r := <-req.resp:
		return r.id, r.deduped, r.err
	case <-ctx.Done():
		return "", false, fmt.Errorf("ingest wait: %w", ctx.Err())
	}
}

// upsert implements the SPEC §1 rule: one row per logical change, keyed by
// dedup_key; an update only applies when the incoming status rank >= stored
// rank, so late "started" events never regress a terminal row.
func (s *Store) upsert(ev *model.Event) (string, bool, error) {
	const q = `
INSERT INTO events (id, ts, ingested_at, source, kind, status, env, cluster,
                    namespace, service, actor, ref, artifact, title, url,
                    duration_ms, dedup_key, payload)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(dedup_key) DO UPDATE SET
  status      = excluded.status,
  ts          = excluded.ts,
  duration_ms = excluded.duration_ms,
  title       = excluded.title,
  url         = excluded.url,
  payload     = excluded.payload
WHERE (CASE excluded.status WHEN 'succeeded' THEN 2 WHEN 'failed' THEN 2 WHEN 'started' THEN 1 ELSE 0 END)
   >= (CASE events.status   WHEN 'succeeded' THEN 2 WHEN 'failed' THEN 2 WHEN 'started' THEN 1 ELSE 0 END)`

	var payload any
	if ev.Payload != "" {
		payload = ev.Payload
	}
	_, err := s.writeDB.Exec(q,
		ev.ID, model.FormatTS(ev.TS), model.FormatTS(ev.IngestedAt),
		string(ev.Source), string(ev.Kind), string(ev.Status),
		ev.Env, ev.Cluster, ev.Namespace, ev.Service, ev.Actor,
		ev.Ref, ev.Artifact, ev.Title, ev.URL,
		ev.DurationMS, ev.DedupKey, payload,
	)
	if err != nil {
		return "", false, fmt.Errorf("upsert event: %w", err)
	}

	// The stored id is authoritative: on conflict the original row keeps its id.
	var storedID string
	if err := s.writeDB.QueryRow(
		`SELECT id FROM events WHERE dedup_key = ?`, ev.DedupKey,
	).Scan(&storedID); err != nil {
		return "", false, fmt.Errorf("read back event id: %w", err)
	}
	return storedID, storedID != ev.ID, nil
}
