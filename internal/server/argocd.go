package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/migueljfsc/wtc/internal/ingest/argocd"
	"github.com/migueljfsc/wtc/internal/store"
)

// handleIngestArgoCD verifies the static X-WTC-Token shared secret, captures
// the raw notification body, and runs the full pipeline: parse → normalize →
// rules → suppress → ingest. Argo CD's notifications-controller templates the
// webhook body per operator (docs/setup/argocd-notifications.yaml ships the
// canonical shape the parser targets) and cannot compute a body HMAC like
// Flux's generic-hmac provider, so auth here is a static shared secret
// compared in constant time.
func (s *Server) handleIngestArgoCD(w http.ResponseWriter, r *http.Request) {
	if s.argocdWebhookToken == "" {
		// Fail closed: without a shared secret we cannot authenticate Argo CD.
		s.writeError(w, http.StatusServiceUnavailable, "argocd webhooks not configured (sources.argocd.webhook_secret)")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		s.writeError(w, http.StatusRequestEntityTooLarge, "body too large or unreadable")
		return
	}

	if !validArgoCDToken(s.argocdWebhookToken, r.Header.Get("X-WTC-Token")) {
		s.writeError(w, http.StatusUnauthorized, "invalid argocd webhook token")
		return
	}

	name := argoCDCaptureName(body)
	s.captureArgoCD(r, name, body)

	n, err := argocd.Parse(body)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ev, facts, suppressKey := argocd.Normalize(n, time.Now())
	if err := s.engine.Apply(ev, facts); err != nil {
		// Rules failing must not drop the event; it lands unenriched.
		s.log.Error("rules apply", "dedup_key", ev.DedupKey, "error", err)
	}

	// Trap #1, Argo edition: resync/refresh re-notifications inside the
	// window are shed before the write path (verified live: unchanged-revision
	// resyncs re-fire Running+Succeeded every time); the strict-rank upsert
	// guarantees one row regardless.
	if s.argocdSuppressor.Suppress(suppressKey, time.Now()) {
		s.writeJSON(w, http.StatusAccepted, map[string]string{"status": argocd.ResultSuppressed})
		return
	}

	id, deduped, err := s.store.Ingest(r.Context(), ev)
	if err != nil {
		switch {
		case errors.Is(err, r.Context().Err()):
			return
		case errors.Is(err, store.ErrStoreClosed):
			s.writeError(w, http.StatusServiceUnavailable, "server is shutting down")
		default:
			s.log.Error("ingest argocd", "error", err)
			s.writeError(w, http.StatusInternalServerError, "storage error")
		}
		return
	}
	s.log.Info("argocd notification", "app", ev.Title, "deduped", deduped)
	code := http.StatusCreated
	if deduped {
		code = http.StatusOK
	}
	s.writeJSON(w, code, IngestResponse{ID: id, Deduped: deduped})
}

// captureArgoCD dumps an argocd delivery's body plus a *safe* header subset.
// Deliberately excludes X-WTC-Token: unlike Flux's X-Signature (a value
// derived from the secret), this header carries the raw shared secret itself
// and must never be written to a fixture on disk.
func (s *Server) captureArgoCD(r *http.Request, name string, body []byte) {
	if s.captureDir == "" {
		return
	}
	headers := map[string]string{}
	for _, h := range []string{"Content-Type", "User-Agent"} {
		if v := r.Header.Get(h); v != "" {
			headers[h] = v
		}
	}
	if err := CaptureBody(s.captureDir, "argocd", name, headers, body); err != nil {
		s.log.Error("capture", "source", "argocd", "error", err)
	}
}

// argoCDCaptureName best-effort extracts app+trigger identity from the
// canonical notification body (docs/setup/argocd-notifications.yaml) so
// captured fixtures get a readable filename. Returns "" on any parse miss —
// CaptureBody's sanitizeFilename falls back to a generated ID, same as every
// other source when identity can't be recovered.
func argoCDCaptureName(body []byte) string {
	var f struct {
		App            string `json:"app"`
		OperationPhase string `json:"operationPhase"`
		SyncStatus     string `json:"syncStatus"`
		HealthStatus   string `json:"healthStatus"`
	}
	if err := json.Unmarshal(body, &f); err != nil || f.App == "" {
		return ""
	}
	trigger := f.OperationPhase
	if trigger == "" {
		trigger = f.SyncStatus
	}
	if trigger == "" {
		trigger = f.HealthStatus
	}
	if trigger == "" {
		return f.App
	}
	return f.App + "-" + trigger
}

// validArgoCDToken compares the static shared-secret header X-WTC-Token in
// constant time. Argo CD's notification templates cannot HMAC-sign the body
// (unlike Flux's generic-hmac provider), so this is the whole auth story for
// /ingest/argocd — see docs/setup/argocd-notifications.yaml.
func validArgoCDToken(secret, header string) bool {
	if header == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(secret), []byte(header)) == 1
}
