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

// statusRecorder captures the response code for request logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
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
