package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/ingest/generic"
	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/query"
	"github.com/migueljfsc/wtc/internal/store"
)

const maxBodyBytes = 1 << 20 // 1 MiB

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAuthVerify lets the portal validate a bearer token without a
// side-effecting call: reaching this handler means requireBearer already
// accepted the token, so a 200 confirms it, a 401 rejects it.
func (s *Server) handleAuthVerify(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// VersionResponse is the /api/v1/version payload (portal Settings tab).
type VersionResponse struct {
	Version string `json:"version"`
}

// handleVersion reports the build-stamped binary version. Bearer-authed like
// the rest of the API — version strings fingerprint deployments, so they are
// not public.
func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, VersionResponse{Version: s.version})
}

// IngestResponse is returned by all ingest endpoints.
type IngestResponse struct {
	ID      string `json:"id"`
	Deduped bool   `json:"deduped"`
}

func (s *Server) handleIngestGeneric(w http.ResponseWriter, r *http.Request) {
	var req generic.Request
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	ev, err := req.ToEvent(time.Now())
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Redaction before storage — hard rule, every ingest path.
	normalize.RedactEvent(ev)

	id, deduped, err := s.store.Ingest(r.Context(), ev)
	if err != nil {
		switch {
		case errors.Is(err, r.Context().Err()):
			return // client went away; nobody to answer
		case errors.Is(err, store.ErrStoreClosed):
			s.writeError(w, http.StatusServiceUnavailable, "server is shutting down")
		default:
			s.log.Error("ingest generic", "error", err)
			s.writeError(w, http.StatusInternalServerError, "storage error")
		}
		return
	}
	code := http.StatusCreated
	if deduped {
		code = http.StatusOK
	}
	s.writeJSON(w, code, IngestResponse{ID: id, Deduped: deduped})
}

func (s *Server) handleWhere(w http.ResponseWriter, r *http.Request) {
	report, err := query.Where(r.Context(), s.store, s.tags.Load(), r.PathValue("ref"))
	if err != nil {
		// Unresolvable refs are no longer errors — Where returns an empty
		// journey with a note. A remaining error is an internal (store) fault.
		s.log.Error("where", "error", err)
		s.writeError(w, http.StatusInternalServerError, "query error")
		return
	}
	s.writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	a, b := r.URL.Query().Get("a"), r.URL.Query().Get("b")
	if a == "" || b == "" || a == b {
		s.writeError(w, http.StatusBadRequest, "diff needs two distinct envs: ?a=staging&b=prod")
		return
	}
	report, err := query.Diff(r.Context(), s.store, a, b)
	if err != nil {
		s.log.Error("diff", "error", err)
		s.writeError(w, http.StatusInternalServerError, "query error")
		return
	}
	s.writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleHandoff(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-7 * 24 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		ts, err := model.ParseTS(v)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "since: "+err.Error())
			return
		}
		since = ts
	}
	report, err := query.Handoff(r.Context(), s.store, since)
	if err != nil {
		s.log.Error("handoff", "error", err)
		s.writeError(w, http.StatusInternalServerError, "query error")
		return
	}
	s.writeJSON(w, http.StatusOK, report)
}

// handleAround returns the changes in a window BEFORE an instant — the
// "what changed right before this alert fired" question. Anchor by ts= or by
// an event id= (typically an alert's).
func (s *Server) handleAround(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var anchor time.Time
	switch {
	case q.Get("id") != "":
		ev, err := s.store.EventByID(r.Context(), q.Get("id"))
		if err != nil {
			s.writeError(w, http.StatusNotFound, "no event with id "+q.Get("id"))
			return
		}
		anchor = ev.TS
	case q.Get("ts") != "":
		ts, err := model.ParseTS(q.Get("ts"))
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "ts: "+err.Error())
			return
		}
		anchor = ts
	default:
		s.writeError(w, http.StatusBadRequest, "around needs ?ts= or ?id=")
		return
	}

	window := 30 * time.Minute
	if v := q.Get("window"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			s.writeError(w, http.StatusBadRequest, "window must be a positive duration")
			return
		}
		window = d
	}

	events, next, err := s.store.ListEvents(r.Context(), store.Filter{
		Since: anchor.Add(-window),
		Until: anchor,
	})
	if err != nil {
		s.log.Error("around", "error", err)
		s.writeError(w, http.StatusInternalServerError, "query error")
		return
	}
	s.writeJSON(w, http.StatusOK, EventsResponse{Events: events, NextCursor: next})
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	report, err := s.store.Doctor(r.Context(), time.Now())
	if err != nil {
		s.log.Error("doctor", "error", err)
		s.writeError(w, http.StatusInternalServerError, "doctor query error")
		return
	}
	// Merge in-memory mapping-webhook eval errors (P14) — they live on the
	// server, not the DB, since they concern deliveries that never became rows.
	report.WebhookMappingErrors = s.mapErrs.snapshot()
	s.writeJSON(w, http.StatusOK, report)
}

// EventsResponse is the paginated /api/events reply.
type EventsResponse struct {
	Events     []model.Event `json:"events"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// csv splits a comma-separated query value into trimmed, non-empty parts.
func csv(v string) []string {
	if v == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
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
		Cursor:   q.Get("cursor"),
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
	if v := q.Get("limit"); v != "" {
		limit, err := strconv.Atoi(v)
		if err != nil || limit < 1 {
			s.writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		f.Limit = limit
	}

	events, next, err := s.store.ListEvents(r.Context(), f)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.log.Error("list events", "error", err)
		s.writeError(w, http.StatusInternalServerError, "query error")
		return
	}
	s.writeJSON(w, http.StatusOK, EventsResponse{Events: events, NextCursor: next})
}
