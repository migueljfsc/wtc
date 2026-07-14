package server

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/migueljfsc/wtc/internal/ingest/alertmanager"
	"github.com/migueljfsc/wtc/internal/store"
)

// handleIngestAlertmanager receives Alertmanager webhook deliveries (bearer
// auth via the webhook's http_config). One event per alert episode:
// firing → started, resolved → succeeded on the same row.
func (s *Server) handleIngestAlertmanager(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		s.writeError(w, http.StatusRequestEntityTooLarge, "body too large or unreadable")
		return
	}
	s.capture(r, "alertmanager", "delivery", body)

	pairs, err := alertmanager.Normalize(body, time.Now())
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	stored := 0
	for _, p := range pairs {
		if err := s.engine.Apply(p.Event, p.Facts); err != nil {
			s.log.Error("rules apply", "dedup_key", p.Event.DedupKey, "error", err)
		}
		if _, _, err := s.store.Ingest(r.Context(), p.Event); err != nil {
			switch {
			case errors.Is(err, r.Context().Err()):
				return
			case errors.Is(err, store.ErrStoreClosed):
				s.writeError(w, http.StatusServiceUnavailable, "server is shutting down")
			default:
				s.log.Error("ingest alertmanager", "error", err)
				s.writeError(w, http.StatusInternalServerError, "storage error")
			}
			return
		}
		stored++
	}
	s.log.Info("alertmanager delivery", "alerts", stored)
	s.writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "alerts": stored})
}
