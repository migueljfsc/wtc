package server

import (
	"net/http"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// defaultStatsWindow is how far back the dashboard aggregations look when the
// caller does not pass ?since=.
const defaultStatsWindow = 30 * 24 * time.Hour

// statsWindow reads optional since/until RFC3339 params, defaulting to the last
// defaultStatsWindow ending now. Returns false after writing a 400 on bad input.
func (s *Server) statsWindow(w http.ResponseWriter, r *http.Request) (since, until time.Time, ok bool) {
	now := time.Now().UTC()
	since, until = now.Add(-defaultStatsWindow), now
	q := r.URL.Query()
	if v := q.Get("since"); v != "" {
		ts, err := model.ParseTS(v)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "since: "+err.Error())
			return since, until, false
		}
		since = ts
	}
	if v := q.Get("until"); v != "" {
		ts, err := model.ParseTS(v)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "until: "+err.Error())
			return since, until, false
		}
		until = ts
	}
	return since, until, true
}

func (s *Server) handleStatsActivity(w http.ResponseWriter, r *http.Request) {
	since, until, ok := s.statsWindow(w, r)
	if !ok {
		return
	}
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		bucket = "day"
	}
	report, err := s.store.ActivityStats(r.Context(), since, until, bucket)
	if err != nil {
		// Bad bucket / oversized window are the client's problem.
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleStatsDeploys(w http.ResponseWriter, r *http.Request) {
	since, until, ok := s.statsWindow(w, r)
	if !ok {
		return
	}
	report, err := s.store.DeployStats(r.Context(), since, until)
	if err != nil {
		s.log.Error("deploy stats", "error", err)
		s.writeError(w, http.StatusInternalServerError, "query error")
		return
	}
	s.writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleFacets(w http.ResponseWriter, r *http.Request) {
	facets, err := s.store.Facets(r.Context())
	if err != nil {
		s.log.Error("facets", "error", err)
		s.writeError(w, http.StatusInternalServerError, "query error")
		return
	}
	s.writeJSON(w, http.StatusOK, facets)
}
