package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
)

// handleIngestGitHub verifies X-Hub-Signature-256, captures the raw delivery,
// and acknowledges it. Parsing/normalization lands with the frozen fixtures
// (CLAUDE.md fixture-first: no normalizer without golden-fixture tests) —
// until then this endpoint exists to authenticate and capture real payloads.
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
	s.log.Info("github delivery", "event", event, "delivery", delivery, "bytes", len(body))

	// Accepted for capture; normalization pipeline arrives in P1 step 2.
	s.writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
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
