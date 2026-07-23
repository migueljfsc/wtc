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
	// Owner is a catalog stamp, not payload inference, so it applies even on
	// this operator-owned path (which skips the rules engine) — service is
	// caller-provided here. Mirrors how redaction runs on every ingest path.
	if s.ownerResolver != nil && ev.Owner == "" {
		ev.Owner = s.ownerResolver(ev.Service, ev.Repo)
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

// parseAt reads the optional ?at= point-in-time param (RFC3339). The zero
// time means "now" (no upper bound). Writes a 400 and returns ok=false on a
// malformed value.
func (s *Server) parseAt(w http.ResponseWriter, r *http.Request) (at time.Time, ok bool) {
	v := r.URL.Query().Get("at")
	if v == "" {
		return time.Time{}, true
	}
	ts, err := model.ParseTS(v)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "at: "+err.Error())
		return time.Time{}, false
	}
	return ts, true
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	a, b := r.URL.Query().Get("a"), r.URL.Query().Get("b")
	if a == "" || b == "" || a == b {
		s.writeError(w, http.StatusBadRequest, "diff needs two distinct envs: ?a=staging&b=prod")
		return
	}
	at, ok := s.parseAt(w, r)
	if !ok {
		return
	}
	report, err := query.Diff(r.Context(), s.store, a, b, at, scopeSvcOwner(r))
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

// handleDORA reports change-failure rate and MTTR over a window (overall, per
// env, per owner). ?window= tunes the deploy→failure attribution span.
func (s *Server) handleDORA(w http.ResponseWriter, r *http.Request) {
	since, until, ok := s.statsWindow(w, r)
	if !ok {
		return
	}
	window := query.DefaultDORAWindow
	if v := r.URL.Query().Get("window"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			s.writeError(w, http.StatusBadRequest, "window: expected a positive duration like 60m")
			return
		}
		window = d
	}
	report, err := query.DORA(r.Context(), s.store, s.tags.Load(), since, until, window, scopeFrom(r))
	if err != nil {
		s.log.Error("dora", "error", err)
		s.writeError(w, http.StatusInternalServerError, "query error")
		return
	}
	s.writeJSON(w, http.StatusOK, report)
}

// handleChangesets lists the logical changes (grouped by app sha) active in a
// window — build → merge → per-env deploys collapsed into one row each.
func (s *Server) handleChangesets(w http.ResponseWriter, r *http.Request) {
	since, until, ok := s.statsWindow(w, r)
	if !ok {
		return
	}
	report, err := query.Changesets(r.Context(), s.store, s.tags.Load(), since, until, scopeFrom(r))
	if err != nil {
		s.log.Error("changesets", "error", err)
		s.writeError(w, http.StatusInternalServerError, "query error")
		return
	}
	s.writeJSON(w, http.StatusOK, report)
}

// resolveAnchor resolves the ?id=/?ts= anchor pair shared by /around and
// /blast: the anchor instant, plus the anchor event when id-anchored (nil for
// a bare ts). On failure it writes the error response and returns ok=false.
func (s *Server) resolveAnchor(w http.ResponseWriter, r *http.Request) (anchor time.Time, ev *model.Event, ok bool) {
	q := r.URL.Query()
	switch {
	case q.Get("id") != "":
		ev, err := s.store.EventByID(r.Context(), q.Get("id"))
		if err != nil {
			s.writeError(w, http.StatusNotFound, "no event with id "+q.Get("id"))
			return time.Time{}, nil, false
		}
		return ev.TS, ev, true
	case q.Get("ts") != "":
		ts, err := model.ParseTS(q.Get("ts"))
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "ts: "+err.Error())
			return time.Time{}, nil, false
		}
		return ts, nil, true
	default:
		s.writeError(w, http.StatusBadRequest, "anchor needs ?ts= or ?id=")
		return time.Time{}, nil, false
	}
}

// parseWindow reads a ?window= duration, writing a 400 on a bad value.
func (s *Server) parseWindow(w http.ResponseWriter, q string, def time.Duration) (time.Duration, bool) {
	if q == "" {
		return def, true
	}
	d, err := time.ParseDuration(q)
	if err != nil || d <= 0 {
		s.writeError(w, http.StatusBadRequest, "window must be a positive duration")
		return 0, false
	}
	return d, true
}

// handleAround returns the changes in a window BEFORE an instant — the
// "what changed right before this alert fired" question. Anchor by ts= or by
// an event id= (typically an alert's).
func (s *Server) handleAround(w http.ResponseWriter, r *http.Request) {
	anchor, _, ok := s.resolveAnchor(w, r)
	if !ok {
		return
	}
	window, ok := s.parseWindow(w, r.URL.Query().Get("window"), 30*time.Minute)
	if !ok {
		return
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

// handleBlast ranks the changes most likely to have caused an alert — or,
// anchored on a change, lists the alerts that fired after it. The
// score is a deterministic documented heuristic; see query.Blast.
func (s *Server) handleBlast(w http.ResponseWriter, r *http.Request) {
	anchorTS, anchorEv, ok := s.resolveAnchor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	window, ok := s.parseWindow(w, q.Get("window"), 2*time.Hour)
	if !ok {
		return
	}
	limit := 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			s.writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = n
	}
	report, err := query.Blast(r.Context(), s.store, query.BlastInput{
		Anchor:  anchorEv,
		TS:      anchorTS,
		Env:     q.Get("env"),
		Service: q.Get("service"),
		Window:  window,
		Limit:   limit,
	})
	if err != nil {
		s.log.Error("blast", "error", err)
		s.writeError(w, http.StatusInternalServerError, "query error")
		return
	}
	s.writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	report, err := s.store.Doctor(r.Context(), time.Now())
	if err != nil {
		s.log.Error("doctor", "error", err)
		s.writeError(w, http.StatusInternalServerError, "doctor query error")
		return
	}
	// Merge in-memory mapping-webhook eval errors — they live on the
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

// scopeFrom reads the global scope facets (env/cluster/service/owner) shared by
// the dashboard, changesets and DORA endpoints.
func scopeFrom(r *http.Request) store.AggScope {
	q := r.URL.Query()
	return store.AggScope{
		Envs:     csv(q.Get("env")),
		Clusters: csv(q.Get("cluster")),
		Services: csv(q.Get("service")),
		Owners:   csv(q.Get("owner")),
	}
}

// scopeSvcOwner reads only service/owner — for diff/matrix, where env is the
// comparison axis (columns), not a scope filter.
func scopeSvcOwner(r *http.Request) store.AggScope {
	q := r.URL.Query()
	return store.AggScope{
		Services: csv(q.Get("service")),
		Owners:   csv(q.Get("owner")),
	}
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.Filter{
		Sources:  csv(q.Get("source")),
		Envs:     csv(q.Get("env")),
		Clusters: csv(q.Get("cluster")),
		Services: csv(q.Get("service")),
		Repos:    csv(q.Get("repo")),
		Owners:   csv(q.Get("owner")),
		Kinds:    csv(q.Get("kind")),
		Statuses: csv(q.Get("status")),
		Actors:   csv(q.Get("actor")),
		Refs:     csv(q.Get("ref")),
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
