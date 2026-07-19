package server

import (
	stdcsv "encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

// P22 "harden the record": explain (inference audit), export (take the
// record with you), backup (keep the record safe). All read-side.

// ExplainReport is the /explain/{id} reply: the per-field first-writer-wins
// trace of how an event got its inferred fields.
type ExplainReport struct {
	EventID string `json:"event_id"`
	Title   string `json:"title"`
	Source  string `json:"source"`
	// Recorded is false for rows ingested before the facts migration or via
	// sources that set fields directly (generic/record/wrap) — no guessing.
	Recorded bool                   `json:"facts_recorded"`
	Facts    *normalize.Facts       `json:"facts,omitempty"`
	Traces   []normalize.FieldTrace `json:"traces,omitempty"`
	Notes    []string               `json:"notes,omitempty"`
}

// handleExplain replays the CURRENT rules (P17 DB overrides included) over an
// event's recorded ingest-time facts and reports which rule set each field.
func (s *Server) handleExplain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ev, err := s.store.EventByID(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "no event with id "+id)
		return
	}

	rep := ExplainReport{EventID: ev.ID, Title: ev.Title, Source: string(ev.Source)}
	if ev.Facts == "" {
		rep.Notes = append(rep.Notes,
			"facts not recorded — the event predates the facts migration, or its source "+
				"(generic/record/wrap) sets fields directly and never runs the rules engine")
		s.writeJSON(w, http.StatusOK, rep)
		return
	}

	facts, preset, err := normalize.DecodeFactsRecord(ev.Facts)
	if err != nil {
		rep.Notes = append(rep.Notes, "stored facts are unreadable: "+err.Error())
		s.writeJSON(w, http.StatusOK, rep)
		return
	}
	traces, err := s.engine.Explain(facts, preset)
	if err != nil {
		s.log.Error("explain", "error", err)
		s.writeError(w, http.StatusInternalServerError, "rules replay error")
		return
	}
	rep.Recorded = true
	rep.Facts = &facts
	rep.Traces = traces

	// The replay ran today's rules; the row was written by ingest-time rules.
	// Surface any divergence instead of letting the trace silently disagree
	// with the stored row.
	for _, t := range traces {
		stored := normalize.FieldValue(ev, t.Field)
		switch {
		case t.Origin == "rule" && stored != t.Value:
			rep.Notes = append(rep.Notes, fmt.Sprintf(
				"%s: current rules produce %q but the stored row has %q — rules changed since ingest",
				t.Field, t.Value, stored))
		case t.Origin == "unmatched" && stored != "":
			rep.Notes = append(rep.Notes, fmt.Sprintf(
				"%s: stored row has %q but current rules set nothing — rule removed since ingest?",
				t.Field, stored))
		}
	}
	s.writeJSON(w, http.StatusOK, rep)
}

// exportColumns is the stable CSV column order — append-only; never reorder,
// exports are consumed by spreadsheets and audit scripts.
var exportColumns = []string{
	"id", "ts", "ingested_at", "source", "kind", "status", "env", "cluster",
	"namespace", "service", "repo", "actor", "ref", "artifact", "title", "url",
	"duration_ms", "dedup_key",
}

func exportRow(ev *model.Event) []string {
	dur := ""
	if ev.DurationMS != nil {
		dur = strconv.FormatInt(*ev.DurationMS, 10)
	}
	return []string{
		ev.ID, model.FormatTS(ev.TS), model.FormatTS(ev.IngestedAt),
		string(ev.Source), string(ev.Kind), string(ev.Status), ev.Env, ev.Cluster,
		ev.Namespace, ev.Service, ev.Repo, ev.Actor, ev.Ref, ev.Artifact,
		ev.Title, ev.URL, dur, ev.DedupKey,
	}
}

// handleExport streams the filtered ledger out — "every prod change in Q3".
// Filters mirror /events; ordering is ts DESC (newest first) like every other
// query. CSV carries the flat columns; ndjson/json carry full events
// (payload + facts included). Pages internally so a large range never
// buffers server-side.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.Filter{
		Sources:  csv(q.Get("source")),
		Envs:     csv(q.Get("env")),
		Services: csv(q.Get("service")),
		Repos:    csv(q.Get("repo")),
		Kinds:    csv(q.Get("kind")),
		Statuses: csv(q.Get("status")),
		Actors:   csv(q.Get("actor")),
		Query:    q.Get("q"),
		Limit:    1000, // page size, not a cap — the loop drains the range
	}
	if v := q.Get("since"); v != "" {
		ts, err := model.ParseTS(v)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "since: "+err.Error())
			return
		}
		f.Since = ts
	}
	if v := q.Get("until"); v != "" {
		ts, err := model.ParseTS(v)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "until: "+err.Error())
			return
		}
		f.Until = ts
	}

	format := q.Get("format")
	if format == "" {
		format = "csv"
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	var write func(*model.Event) error
	var finish func() error

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="wtc-export-`+stamp+`.csv"`)
		cw := stdcsv.NewWriter(w)
		if err := cw.Write(exportColumns); err != nil {
			return // client went away
		}
		write = func(ev *model.Event) error { return cw.Write(exportRow(ev)) }
		finish = func() error { cw.Flush(); return cw.Error() }
	case "ndjson":
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="wtc-export-`+stamp+`.ndjson"`)
		enc := json.NewEncoder(w)
		write = func(ev *model.Event) error { return enc.Encode(ev) }
		finish = func() error { return nil }
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="wtc-export-`+stamp+`.json"`)
		first := true
		if _, err := io.WriteString(w, "["); err != nil {
			return
		}
		write = func(ev *model.Event) error {
			sep := ",\n"
			if first {
				sep, first = "", false
			}
			b, err := json.Marshal(ev)
			if err != nil {
				return err
			}
			_, err = io.WriteString(w, sep+string(b))
			return err
		}
		finish = func() error { _, err := io.WriteString(w, "]\n"); return err }
	default:
		s.writeError(w, http.StatusBadRequest, "format must be csv, ndjson or json")
		return
	}

	// Headers are sent with the first page — errors after that can only be
	// logged and the stream cut short; the CSV/JSON tail makes truncation
	// detectable client-side.
	for {
		events, next, err := s.store.ListEvents(r.Context(), f)
		if err != nil {
			s.log.Error("export", "error", err)
			return
		}
		for i := range events {
			if err := write(&events[i]); err != nil {
				return // client went away
			}
		}
		if next == "" {
			break
		}
		f.Cursor = next
	}
	if err := finish(); err != nil {
		s.log.Error("export finish", "error", err)
	}
}

// handleBackup streams a consistent point-in-time snapshot of the sqlite
// ledger, taken with VACUUM INTO while serving (WAL-safe). The CLI never
// opens the DB and may be remote, so the snapshot is written to a server-side
// temp file, streamed, and deleted. Postgres deployments back up with
// pg_dump — see docs/setup/backup.md.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("wtc-backup-%d.db", time.Now().UnixNano()))
	defer func() { _ = os.Remove(tmp) }()

	if err := s.store.BackupInto(r.Context(), tmp); err != nil {
		if errors.Is(err, store.ErrBackupUnsupported) {
			s.writeError(w, http.StatusNotImplemented,
				"backup is for the sqlite backend — back up postgres with pg_dump (docs/setup/backup.md)")
			return
		}
		s.log.Error("backup", "error", err)
		s.writeError(w, http.StatusInternalServerError, "backup error")
		return
	}

	fh, err := os.Open(tmp)
	if err != nil {
		s.log.Error("backup open", "error", err)
		s.writeError(w, http.StatusInternalServerError, "backup error")
		return
	}
	defer func() { _ = fh.Close() }()
	st, err := fh.Stat()
	if err != nil {
		s.log.Error("backup stat", "error", err)
		s.writeError(w, http.StatusInternalServerError, "backup error")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	w.Header().Set("Content-Disposition",
		`attachment; filename="wtc-`+time.Now().UTC().Format("20060102-150405")+`.db"`)
	if _, err := io.Copy(w, fh); err != nil {
		s.log.Error("backup stream", "error", err) // client went away mid-copy
	}
}
