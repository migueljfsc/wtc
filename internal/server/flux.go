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

	"github.com/migueljfsc/wtc/internal/ingest/flux"
	"github.com/migueljfsc/wtc/internal/store"
)

// handleIngestFlux verifies the notification-controller generic-hmac
// signature (X-Signature: sha256=<hex>, pinned by captured deliveries) and
// runs the full pipeline: parse → normalize → rules → suppress → ingest.
func (s *Server) handleIngestFlux(w http.ResponseWriter, r *http.Request) {
	if s.fluxHMACKey == "" {
		s.writeError(w, http.StatusServiceUnavailable, "flux ingest not configured (sources.flux.hmac_key)")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		s.writeError(w, http.StatusRequestEntityTooLarge, "body too large or unreadable")
		return
	}

	if !validFluxSignature(s.fluxHMACKey, r.Header.Get("X-Signature"), body) {
		// Capture even rejected deliveries (marked) so a mistaken assumption
		// about the header format is visible in the capture dir instead of
		// silently dropping everything. Auth still fails closed.
		s.captureFlux(r, "unverified", body)
		s.writeError(w, http.StatusUnauthorized, "invalid flux signature")
		return
	}
	s.captureFlux(r, "event", body)

	fe, err := flux.Parse(body)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ev, facts := flux.Normalize(fe, time.Now())
	if ev == nil {
		// Progressing pre-events carry no outcome; the reconcile result
		// arrives seconds later.
		s.writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
		return
	}
	if err := s.engine.Apply(ev, facts); err != nil {
		// Rules failing must not drop the event; it lands unenriched.
		s.log.Error("rules apply", "dedup_key", ev.DedupKey, "error", err)
	}

	// Trap #1: reconcile re-emits inside the window are shed before the
	// write path; the strict-rank upsert guarantees one row regardless.
	if s.fluxSuppressor.Suppress(ev.DedupKey, time.Now()) {
		s.writeJSON(w, http.StatusAccepted, map[string]string{"status": flux.ResultSuppressed})
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
			s.log.Error("ingest flux", "error", err)
			s.writeError(w, http.StatusInternalServerError, "storage error")
		}
		return
	}
	s.log.Info("flux event", "object", ev.Title, "deduped", deduped)
	code := http.StatusCreated
	if deduped {
		code = http.StatusOK
	}
	s.writeJSON(w, code, IngestResponse{ID: id, Deduped: deduped})
}

func (s *Server) captureFlux(r *http.Request, name string, body []byte) {
	if s.captureDir == "" {
		return
	}
	headers := map[string]string{}
	for _, h := range []string{"Content-Type", "X-Signature", "User-Agent", "Gotk-Component"} {
		if v := r.Header.Get(h); v != "" {
			headers[h] = v
		}
	}
	if err := CaptureBody(s.captureDir, "flux", name, headers, body); err != nil {
		s.log.Error("capture", "source", "flux", "error", err)
	}
}

// validFluxSignature checks the generic-hmac provider's X-Signature header:
// HMAC-SHA256 over the raw body, hex-encoded, with or without a "sha256="
// prefix (accept both until the real format is pinned by fixtures).
func validFluxSignature(key, header string, body []byte) bool {
	sig := strings.TrimPrefix(header, "sha256=")
	if sig == "" {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
