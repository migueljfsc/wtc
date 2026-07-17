package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/ingest/github"
	"github.com/migueljfsc/wtc/internal/store"
)

// githubIngestResponse is the /ingest/github reply. A push hook carries many
// commits (N events), so the response is a count rather than the single-event
// IngestResponse the GitOps endpoints return.
type githubIngestResponse struct {
	Ingested int `json:"ingested"`
	Deduped  int `json:"deduped"`
}

// handleIngestGitHub verifies X-Hub-Signature-256, captures the raw delivery,
// then parses the event envelope and runs the full pipeline: normalize → rules
// → ingest. Webhook and poller converge on the same Events + dedup keys (P13),
// so both modes can run simultaneously — the webhook gives latency, the poller
// remains the idempotent loss-recovery sweeper.
func (s *Server) handleIngestGitHub(w http.ResponseWriter, r *http.Request) {
	if s.webhookSecret == "" {
		// Fail closed: without a shared secret we cannot authenticate GitHub.
		s.writeError(w, http.StatusServiceUnavailable, "github webhooks not configured (sources.github.webhook_secret)")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		s.writeError(w, http.StatusRequestEntityTooLarge, "body too large or unreadable")
		return
	}

	if !validGitHubSignature(s.webhookSecret, r.Header.Get("X-Hub-Signature-256"), body) {
		s.writeError(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}

	event := r.Header.Get("X-GitHub-Event")
	delivery := r.Header.Get("X-GitHub-Delivery")
	s.capture(r, "github", event+"-"+delivery, body)

	pairs, err := github.ParseWebhook(event, body, time.Now())
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
				s.log.Error("ingest github", "error", err)
				s.writeError(w, http.StatusInternalServerError, "storage error")
			}
			return
		}
		ingested++
		if dup {
			deduped++
		}
	}

	s.log.Info("github delivery", "event", event, "delivery", delivery, "ingested", ingested, "deduped", deduped)
	// 202 when the delivery carried nothing we model (ping, non-merge PR); 201
	// when at least one new row landed; 200 when everything deduped.
	code := http.StatusAccepted
	switch {
	case ingested > deduped:
		code = http.StatusCreated
	case ingested > 0:
		code = http.StatusOK
	}
	s.writeJSON(w, code, githubIngestResponse{Ingested: ingested, Deduped: deduped})
}

// validGitHubSignature checks GitHub's X-Hub-Signature-256 header:
// "sha256=" + hex(HMAC-SHA256(secret, body)), compared in constant time.
func validGitHubSignature(secret, header string, body []byte) bool {
	sig, ok := strings.CutPrefix(header, "sha256=")
	if !ok || sig == "" {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
