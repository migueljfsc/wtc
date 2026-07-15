package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

// requireBearer enforces static bearer-token auth. Comparison is
// constant-time per token; an empty token list denies everything.
func (s *Server) requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			s.writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		for _, want := range s.tokens {
			if subtle.ConstantTimeCompare([]byte(token), []byte(want)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}
		s.writeError(w, http.StatusUnauthorized, "invalid bearer token")
	})
}

// withCORS emits cross-origin headers for the portal SPA and answers
// preflight OPTIONS requests. Off (no headers) when no origins are configured;
// a lone "*" allows any origin. The Origin is always echoed (never a literal
// "*") with Vary: Origin, so responses stay correct behind shared caches and a
// future credentialed mode keeps working.
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.originAllowed(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed reports whether origin is permitted by the configured
// allow-list. A single "*" entry permits any origin.
func (s *Server) originAllowed(origin string) bool {
	for _, o := range s.corsOrigins {
		if o == "*" || o == origin {
			return true
		}
	}
	return false
}

// statusRecorder captures the response code for request logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the underlying writer so SSE handlers (which type-assert
// http.Flusher) work through the logging wrapper.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		s.log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
