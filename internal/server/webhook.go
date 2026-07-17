package server

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/migueljfsc/wtc/internal/ingest/mapping"
	"github.com/migueljfsc/wtc/internal/metrics"
	"github.com/migueljfsc/wtc/internal/store"
)

// mappingErrorTracker records the most recent per-source mapping-template
// failure so doctor can surface it (P14 guardrail). In-memory and reset on
// restart: it is a "recent health" signal, not an audit log. A template that
// fails to render must never silently drop a delivery unnoticed — the delivery
// is rejected (the sender can retry) AND the failure is counted here.
type mappingErrorTracker struct {
	mu sync.Mutex
	by map[string]*store.WebhookMappingError
}

func newMappingErrorTracker() *mappingErrorTracker {
	return &mappingErrorTracker{by: map[string]*store.WebhookMappingError{}}
}

func (t *mappingErrorTracker) record(source string, err error) {
	metrics.MappingErrors.WithLabelValues(source).Inc()
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.by[source]
	if e == nil {
		e = &store.WebhookMappingError{Source: source}
		t.by[source] = e
	}
	e.Count++
	e.LastError = err.Error()
	e.LastAt = time.Now().UTC()
}

// snapshot returns a stable copy of the recorded errors for the doctor report.
func (t *mappingErrorTracker) snapshot() []store.WebhookMappingError {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.by) == 0 {
		return nil
	}
	out := make([]store.WebhookMappingError, 0, len(t.by))
	for _, e := range t.by {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

// webhookIngestResponse is the /ingest/webhook/<name> reply.
type webhookIngestResponse struct {
	Ingested int `json:"ingested"`
	Deduped  int `json:"deduped"`
}

// handleIngestWebhook is the config-declared mapping webhook (P14). It resolves
// the source by path name, authenticates (static token or HMAC per the source's
// config), captures the raw body, then maps payload→Event via the operator's
// templates and runs the standard pipeline (rules → redaction → upsert).
func (s *Server) handleIngestWebhook(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	m, ok := s.mappers[name]
	if !ok {
		s.writeError(w, http.StatusNotFound, "no mapping webhook named "+name)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		s.writeError(w, http.StatusRequestEntityTooLarge, "body too large or unreadable")
		return
	}

	if !authorizeWebhook(m.AuthConfig(), r, body) {
		s.writeError(w, http.StatusUnauthorized, "webhook authentication failed")
		return
	}

	s.captureWebhook(r, name, body)

	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		s.writeError(w, http.StatusBadRequest, "body is not valid JSON")
		return
	}

	pf, err := m.Normalize(root, time.Now())
	if err != nil {
		// Template/mapping failure: surface it (doctor) and reject so the sender
		// can retry — never a silent drop.
		s.mapErrs.record(name, err)
		s.log.Error("mapping webhook", "source", name, "error", err)
		s.writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if err := s.engine.Apply(pf.Event, pf.Facts); err != nil {
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
			s.log.Error("ingest webhook", "source", name, "error", err)
			s.writeError(w, http.StatusInternalServerError, "storage error")
		}
		return
	}

	deduped := 0
	code := http.StatusCreated
	if dup {
		deduped = 1
		code = http.StatusOK
	}
	s.log.Info("webhook delivery", "source", name, "deduped", dup)
	s.writeJSON(w, code, webhookIngestResponse{Ingested: 1, Deduped: deduped})
}

// authorizeWebhook verifies a mapping webhook request against its configured
// auth: a static shared secret in a header (constant-time compare, default
// X-WTC-Token) or an HMAC the sender computed over the raw body.
func authorizeWebhook(a mapping.Auth, r *http.Request, body []byte) bool {
	if a.HMAC != nil && a.HMAC.Secret != "" {
		got := r.Header.Get(a.HMAC.Header)
		got = strings.TrimPrefix(got, a.HMAC.Prefix)
		return validHexHMAC(a.HMAC.Algo, a.HMAC.Secret, got, body)
	}
	if a.Token != "" {
		header := a.Header
		if header == "" {
			header = "X-WTC-Token"
		}
		return subtle.ConstantTimeCompare([]byte(a.Token), []byte(r.Header.Get(header))) == 1
	}
	return false // fail closed
}

// validHexHMAC computes HMAC-<algo> over body and constant-time compares it to
// the hex signature got. Empty got fails. Hex encoding covers our target
// senders (github/tfc/harbor); base64 senders are out of scope for v1.
func validHexHMAC(algo, secret, got string, body []byte) bool {
	if got == "" {
		return false
	}
	var h func() hash.Hash
	switch algo {
	case "", "sha256":
		h = sha256.New
	case "sha512":
		h = sha512.New
	case "sha1":
		h = sha1.New
	default:
		return false
	}
	mac := hmac.New(h, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(got)), []byte(want)) == 1
}

// captureWebhook dumps a mapping-webhook delivery body plus a safe header
// subset. Auth headers (static token / HMAC signature) are deliberately
// excluded — like gitlab/argocd, they carry the shared secret.
func (s *Server) captureWebhook(r *http.Request, name string, body []byte) {
	if s.captureDir == "" {
		return
	}
	headers := map[string]string{}
	for _, h := range []string{"Content-Type", "User-Agent"} {
		if v := r.Header.Get(h); v != "" {
			headers[h] = v
		}
	}
	if err := CaptureBody(s.captureDir, "webhook", name, headers, body); err != nil {
		s.log.Error("capture", "source", "webhook:"+name, "error", err)
	}
}
