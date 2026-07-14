// Package server exposes the ingest and query HTTP API served by `wtc serve`.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/migueljfsc/wtc/internal/ingest/flux"
	"github.com/migueljfsc/wtc/internal/normalize"
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
	// FluxHMACKey enables /ingest/flux generic-hmac verification; empty
	// means the endpoint rejects everything (fail closed).
	FluxHMACKey string
	// FluxSuppression drops repeats of the same flux dedup key inside this
	// window (trap #1). <= 0 disables.
	FluxSuppression time.Duration
	// Engine runs the normalization rules on webhook-ingested events; nil
	// means an empty rule set.
	Engine *normalize.Engine
	// Tags resolves image tags to shas for /api/where; nil means defaults.
	Tags *normalize.TagResolver
	// CaptureDir, when non-empty, dumps every raw ingest body to disk.
	CaptureDir string
}

// Server routes ingest and query requests onto a Store.
type Server struct {
	store          *store.Store
	tokens         []string
	webhookSecret  string
	fluxHMACKey    string
	fluxSuppressor *flux.Suppressor
	engine         *normalize.Engine
	tags           *normalize.TagResolver
	captureDir     string
	log            *slog.Logger
	mux            *http.ServeMux
}

// New builds the HTTP surface.
func New(st *store.Store, opts Options, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	engine := opts.Engine
	if engine == nil {
		engine, _ = normalize.NewEngine(nil) // empty rule set cannot fail
	}
	tags := opts.Tags
	if tags == nil {
		tags, _ = normalize.NewTagResolver(nil) // defaults cannot fail
	}
	s := &Server{
		store:          st,
		tokens:         opts.Tokens,
		webhookSecret:  opts.GitHubWebhookSecret,
		fluxHMACKey:    opts.FluxHMACKey,
		fluxSuppressor: flux.NewSuppressor(opts.FluxSuppression),
		engine:         engine,
		tags:           tags,
		captureDir:     opts.CaptureDir,
		log:            log,
		mux:            http.NewServeMux(),
	}

	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.Handle("POST /ingest/generic", s.requireBearer(http.HandlerFunc(s.handleIngestGeneric)))
	s.mux.HandleFunc("POST /ingest/github", s.handleIngestGitHub) // HMAC-verified inside
	s.mux.HandleFunc("POST /ingest/flux", s.handleIngestFlux)     // HMAC-verified inside
	s.mux.Handle("GET /api/events", s.requireBearer(http.HandlerFunc(s.handleListEvents)))
	s.mux.Handle("GET /api/doctor", s.requireBearer(http.HandlerFunc(s.handleDoctor)))
	s.mux.Handle("GET /api/where/{ref}", s.requireBearer(http.HandlerFunc(s.handleWhere)))
	s.mux.Handle("GET /api/diff", s.requireBearer(http.HandlerFunc(s.handleDiff)))
	s.mux.Handle("GET /api/handoff", s.requireBearer(http.HandlerFunc(s.handleHandoff)))

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
