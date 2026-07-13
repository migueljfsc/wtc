package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/migueljfsc/wtc/internal/ingest/generic"
	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

const maxBodyBytes = 1 << 20 // 1 MiB

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

// EventsResponse is the paginated /api/events reply.
type EventsResponse struct {
	Events     []model.Event `json:"events"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("q") != "" {
		// SPEC §4 lists q (FTS) but it lands in Phase 3; silently returning
		// unfiltered results would be wrong data, so reject loudly.
		s.writeError(w, http.StatusBadRequest, "q (full-text search) is not implemented yet — lands in phase 3")
		return
	}

	f := store.Filter{
		Env:     q.Get("env"),
		Service: q.Get("service"),
		Kind:    q.Get("kind"),
		Status:  q.Get("status"),
		Cursor:  q.Get("cursor"),
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
