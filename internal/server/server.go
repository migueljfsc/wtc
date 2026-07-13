// Package server exposes the ingest and query HTTP API served by `wtc serve`.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/migueljfsc/wtc/internal/store"
)

// Options configures the HTTP surface beyond the store itself.
type Options struct {
	// Tokens are the static bearer tokens accepted on /api/* and
	// /ingest/generic; an empty list fails closed (all denied).
	Tokens []string
	// GitHubWebhookSecret enables /ingest/github HMAC verification; empty
	// means the endpoint rejects everything (fail closed).
	GitHubWebhookSecret string
	// CaptureDir, when non-empty, dumps every raw ingest body to disk.
	CaptureDir string
}

// Server routes ingest and query requests onto a Store.
type Server struct {
	store         *store.Store
	tokens        []string
	webhookSecret string
	captureDir    string
	log           *slog.Logger
	mux           *http.ServeMux
}

// New builds the HTTP surface.
func New(st *store.Store, opts Options, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		store:         st,
		tokens:        opts.Tokens,
		webhookSecret: opts.GitHubWebhookSecret,
		captureDir:    opts.CaptureDir,
		log:           log,
		mux:           http.NewServeMux(),
	}

	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.Handle("POST /ingest/generic", s.requireBearer(http.HandlerFunc(s.handleIngestGeneric)))
	s.mux.HandleFunc("POST /ingest/github", s.handleIngestGitHub) // HMAC-verified inside
	s.mux.Handle("GET /api/events", s.requireBearer(http.HandlerFunc(s.handleListEvents)))
	s.mux.Handle("GET /api/doctor", s.requireBearer(http.HandlerFunc(s.handleDoctor)))

	return s
}

// Handler returns the root handler (with request logging).
func (s *Server) Handler() http.Handler {
	return s.logRequests(s.mux)
}

// ErrorResponse is the wire shape of every error reply. Shared with the CLI
// client so the contract cannot drift.
type ErrorResponse struct {
	Error string `json:"error"`
}

func (s *Server) writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.log.Error("write response", "error", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, code int, msg string) {
	s.writeJSON(w, code, ErrorResponse{Error: msg})
}
