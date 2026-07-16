package server

import (
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/migueljfsc/wtc/internal/ingest/gitlab"
	"github.com/migueljfsc/wtc/internal/store"
)

// gitlabIngestResponse is the /ingest/gitlab reply. A push hook carries many
// commits (N events), so the response is a count rather than the single-event
// IngestResponse the other endpoints return.
type gitlabIngestResponse struct {
	Ingested int `json:"ingested"`
	Deduped  int `json:"deduped"`
}

// handleIngestGitLab verifies the static X-Gitlab-Token shared secret, captures
// the raw delivery, and runs the full pipeline: parse → normalize → rules →
// ingest. GitLab does not HMAC-sign webhook bodies (it sends the secret
// verbatim in X-Gitlab-Token), so auth is a constant-time compare of that
// header — the same shape as /ingest/argocd. The poller (outbound API) is the
// primary path for private deployments; this endpoint is the peer webhook mode
// and converges on the same Events + dedup keys, so both can run at once.
func (s *Server) handleIngestGitLab(w http.ResponseWriter, r *http.Request) {
	if s.gitlabWebhookToken == "" {
		// Fail closed: without a shared secret we cannot authenticate GitLab.
		s.writeError(w, http.StatusServiceUnavailable, "gitlab webhooks not configured (sources.gitlab.webhook_secret)")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		s.writeError(w, http.StatusRequestEntityTooLarge, "body too large or unreadable")
		return
	}

	if !validGitLabToken(s.gitlabWebhookToken, r.Header.Get("X-Gitlab-Token")) {
		s.writeError(w, http.StatusUnauthorized, "invalid gitlab webhook token")
		return
	}

	event := r.Header.Get("X-Gitlab-Event")
	s.captureGitLab(r, event, body)

	pairs, err := gitlab.ParseWebhook(body, time.Now())
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var ingested, deduped int
	for _, pf := range pairs {
		if err := s.engine.Apply(pf.Event, pf.Facts); err != nil {
			// Rules failing must not drop the event; it lands unenriched.
			s.log.Error("rules apply", "dedup_key", pf.Event.DedupKey, "error", err)
		}
		_, dup, err := s.store.Ingest(r.Context(), pf.Event)
		if err != nil {
			switch {
			case errors.Is(err, r.Context().Err()):
				return
			case errors.Is(err, store.ErrStoreClosed):
				s.writeError(w, http.StatusServiceUnavailable, "server is shutting down")
			default:
				s.log.Error("ingest gitlab", "error", err)
				s.writeError(w, http.StatusInternalServerError, "storage error")
			}
			return
		}
		ingested++
		if dup {
			deduped++
		}
	}

	s.log.Info("gitlab delivery", "event", event, "ingested", ingested, "deduped", deduped)
	// 202 when the hook carried nothing we model (e.g. a non-merge MR action);
	// 201 when at least one new row landed; 200 when everything deduped.
	code := http.StatusAccepted
	switch {
	case ingested > deduped:
		code = http.StatusCreated
	case ingested > 0:
		code = http.StatusOK
	}
	s.writeJSON(w, code, gitlabIngestResponse{Ingested: ingested, Deduped: deduped})
}

// captureGitLab dumps a gitlab delivery's body plus a safe header subset.
// Deliberately excludes X-Gitlab-Token: like Argo's X-WTC-Token it carries the
// raw shared secret and must never be written to a fixture on disk.
func (s *Server) captureGitLab(r *http.Request, event string, body []byte) {
	if s.captureDir == "" {
		return
	}
	headers := map[string]string{}
	for _, h := range []string{"Content-Type", "X-Gitlab-Event", "X-Gitlab-Instance", "User-Agent"} {
		if v := r.Header.Get(h); v != "" {
			headers[h] = v
		}
	}
	name := event
	if name == "" {
		name = "gitlab"
	}
	if err := CaptureBody(s.captureDir, "gitlab", name, headers, body); err != nil {
		s.log.Error("capture", "source", "gitlab", "error", err)
	}
}

// validGitLabToken compares the static shared-secret header X-Gitlab-Token in
// constant time. GitLab's webhooks cannot HMAC-sign the body (unlike Flux's
// generic-hmac provider), so this is the whole auth story for /ingest/gitlab —
// see docs/setup/gitlab.md.
func validGitLabToken(secret, header string) bool {
	if header == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(secret), []byte(header)) == 1
}
