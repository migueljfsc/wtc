package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/migueljfsc/wtc/internal/ingest/generic"
	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/store"
)

const maxBodyBytes = 1 << 20 // 1 MiB

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.log, http.StatusOK, map[string]string{"status": "ok"})
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

	id, deduped, err := s.store.Ingest(r.Context(), ev)
	if err != nil {
		if errors.Is(err, r.Context().Err()) {
			return // client went away
		}
		s.log.Error("ingest generic", "error", err)
		s.writeError(w, http.StatusInternalServerError, "storage error")
		return
	}
	code := http.StatusCreated
	if deduped {
		code = http.StatusOK
	}
	writeJSON(w, s.log, code, IngestResponse{ID: id, Deduped: deduped})
}

// EventsResponse is the paginated /api/events reply.
type EventsResponse struct {
	Events     []model.Event `json:"events"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
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
		s.log.Error("list events", "error", err)
		s.writeError(w, http.StatusInternalServerError, "query error")
		return
	}
	writeJSON(w, s.log, http.StatusOK, EventsResponse{Events: events, NextCursor: next})
}
