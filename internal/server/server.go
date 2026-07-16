// Package server exposes the ingest and query HTTP API served by `wtc serve`.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/migueljfsc/wtc/internal/ingest/flux"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
	"github.com/migueljfsc/wtc/web"
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
	// ArgoCDWebhookToken enables /ingest/argocd static shared-secret
	// verification (X-WTC-Token, constant-time compare); empty means the
	// endpoint rejects everything (fail closed). Argo's notification
	// templates cannot HMAC-sign the body like Flux's generic-hmac provider.
	ArgoCDWebhookToken string
	// FluxSuppression drops repeats of the same flux dedup key inside this
	// window (trap #1). <= 0 disables.
	FluxSuppression time.Duration
	// ArgoCDSuppression drops repeats of the same argocd suppression key
	// (app+revision+phase|health) inside this window — Argo re-notifies on
	// every resync, same trap as Flux. <= 0 disables.
	ArgoCDSuppression time.Duration
	// Engine runs the normalization rules on webhook-ingested events; nil
	// means an empty rule set. A holder so rules can be hot-reloaded (P10) —
	// the same holder must be shared with the poller.
	Engine *normalize.EngineHolder
	// Tags resolves image tags to shas for /api/where; nil means defaults.
	Tags *normalize.TagResolverHolder
	// CaptureDir, when non-empty, dumps every raw ingest body to disk.
	CaptureDir string
	// CORSAllowedOrigins enables cross-origin access from the portal SPA. Empty
	// means CORS is off (no headers emitted); a single "*" allows any origin.
	CORSAllowedOrigins []string
	// Rules and TagPatterns are exposed read-only at /api/v1/config (portal
	// config viewer). Display copies of what the engine/resolver were built from.
	Rules       []normalize.Rule
	TagPatterns []string
}

// Server routes ingest and query requests onto a Store.
type Server struct {
	store              *store.Store
	tokens             []string
	webhookSecret      string
	fluxHMACKey        string
	argocdWebhookToken string
	fluxSuppressor     *flux.Suppressor
	argocdSuppressor   *flux.Suppressor // same window mechanism; keys are argocd-shaped
	engine             *normalize.EngineHolder
	tags               *normalize.TagResolverHolder
	captureDir         string
	corsOrigins        []string
	log                *slog.Logger
	mux                *http.ServeMux

	// Editable normalization config (P10). fileRules/fileTags are the YAML
	// baseline; cfg* is the effective, possibly DB-overridden, snapshot served
	// at /config and mutated by the edit endpoints. cfgMu serializes edits.
	fileRules   []normalize.Rule
	fileTags    []string
	cfgMu       sync.Mutex
	curRules    []normalize.Rule
	curTags     []string
	rulesFromDB bool
	tagsFromDB  bool
}

// New builds the HTTP surface.
func New(st *store.Store, opts Options, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	engine := opts.Engine
	if engine == nil {
		e, _ := normalize.NewEngine(nil) // empty rule set cannot fail
		engine = normalize.NewEngineHolder(e)
	}
	tags := opts.Tags
	if tags == nil {
		t, _ := normalize.NewTagResolver(nil) // defaults cannot fail
		tags = normalize.NewTagResolverHolder(t)
	}
	s := &Server{
		store:              st,
		tokens:             opts.Tokens,
		webhookSecret:      opts.GitHubWebhookSecret,
		fluxHMACKey:        opts.FluxHMACKey,
		argocdWebhookToken: opts.ArgoCDWebhookToken,
		fluxSuppressor:     flux.NewSuppressor(opts.FluxSuppression),
		argocdSuppressor:   flux.NewSuppressor(opts.ArgoCDSuppression),
		engine:             engine,
		tags:               tags,
		captureDir:         opts.CaptureDir,
		corsOrigins:        opts.CORSAllowedOrigins,
		fileRules:          opts.Rules,
		fileTags:           opts.TagPatterns,
		curRules:           opts.Rules,
		curTags:            opts.TagPatterns,
		log:                log,
		mux:                http.NewServeMux(),
	}

	// Apply any DB-backed config overrides (P10), rebuilding + swapping the
	// engine/resolver holders before ingest starts. Failures fall back to the
	// YAML baseline (already installed) and are logged.
	s.loadConfigOverrides()

	// Embedded UI at the root. Registered routes below win over this
	// catch-all (Go 1.22 mux precedence), so the API is never shadowed.
	// Static assets are public; every data call they make is bearer-authed.
	s.mux.Handle("GET /", http.FileServerFS(web.FS()))

	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	// OpenAPI document for the portal's typed client generator. Public, like
	// healthz — it describes the (bearer-authed) API but leaks no data.
	s.mux.HandleFunc("GET /api/openapi.json", s.handleOpenAPI)
	s.mux.Handle("POST /ingest/generic", s.requireBearer(http.HandlerFunc(s.handleIngestGeneric)))
	s.mux.Handle("POST /ingest/alertmanager", s.requireBearer(http.HandlerFunc(s.handleIngestAlertmanager)))
	s.mux.HandleFunc("POST /ingest/github", s.handleIngestGitHub) // HMAC-verified inside
	s.mux.HandleFunc("POST /ingest/flux", s.handleIngestFlux)     // HMAC-verified inside
	s.mux.HandleFunc("POST /ingest/argocd", s.handleIngestArgoCD) // token-verified inside

	// Query API. Every route is registered under both the legacy /api prefix
	// (CLI client + embedded web depend on it) and the versioned /api/v1
	// prefix (the portal SPA's client) — same handler, so the two can never
	// drift. apiRoutes() is the single source the OpenAPI drift test checks.
	for _, rt := range s.apiRoutes() {
		h := s.requireBearer(rt.handler)
		s.mux.Handle(rt.method+" /api"+rt.path, h)
		s.mux.Handle(rt.method+" /api/v1"+rt.path, h)
	}

	return s
}

// apiRoute is one query-API endpoint, registered under both /api and /api/v1.
// The path is relative to the version prefix and uses Go 1.22 mux wildcards
// ({ref}), which are identical to OpenAPI path templating.
type apiRoute struct {
	method  string
	path    string
	handler http.Handler
}

// apiRoutes is the authoritative list of query-API endpoints. The OpenAPI
// drift test asserts every entry here is documented in openapi.json, so a new
// route cannot ship without its contract.
func (s *Server) apiRoutes() []apiRoute {
	return []apiRoute{
		{"GET", "/events", http.HandlerFunc(s.handleListEvents)},
		{"GET", "/doctor", http.HandlerFunc(s.handleDoctor)},
		{"GET", "/where/{ref}", http.HandlerFunc(s.handleWhere)},
		{"GET", "/around", http.HandlerFunc(s.handleAround)},
		{"GET", "/diff", http.HandlerFunc(s.handleDiff)},
		{"GET", "/handoff", http.HandlerFunc(s.handleHandoff)},
		{"GET", "/stats/activity", http.HandlerFunc(s.handleStatsActivity)},
		{"GET", "/stats/deploys", http.HandlerFunc(s.handleStatsDeploys)},
		{"GET", "/facets", http.HandlerFunc(s.handleFacets)},
		{"GET", "/matrix", http.HandlerFunc(s.handleMatrix)},
		{"GET", "/config", http.HandlerFunc(s.handleConfig)},
		{"PUT", "/config/rules", http.HandlerFunc(s.handlePutRules)},
		{"DELETE", "/config/rules", http.HandlerFunc(s.handleResetRules)},
		{"PUT", "/config/tag_patterns", http.HandlerFunc(s.handlePutTagPatterns)},
		{"DELETE", "/config/tag_patterns", http.HandlerFunc(s.handleResetTagPatterns)},
		{"GET", "/stream", http.HandlerFunc(s.handleStream)},
		{"GET", "/auth/verify", http.HandlerFunc(s.handleAuthVerify)},
	}
}

// Handler returns the root handler (request logging + CORS).
func (s *Server) Handler() http.Handler {
	return s.logRequests(s.withCORS(s.mux))
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
