package server

import (
	"net/http"

	"github.com/migueljfsc/wtc/internal/capture"
)

// Capture mode (the fixture-first workflow): when a capture directory
// is configured, every raw ingest body plus its headers is dumped to disk so
// real payloads can be frozen into testdata/ fixtures. Dev-only by design.

// CaptureBody is retained as the server-side spelling of capture.Body; the
// implementation lives in internal/capture so ingest packages can capture
// without importing server (avoiding an import cycle).
func CaptureBody(dir, source, name string, headers map[string]string, body []byte) error {
	return capture.Body(dir, source, name, headers, body)
}

// capture dumps an incoming ingest request's body and interesting headers.
// Never fails the request.
func (s *Server) capture(r *http.Request, source, name string, body []byte) {
	if s.captureDir == "" {
		return
	}
	headers := map[string]string{}
	for _, h := range []string{"Content-Type", "X-GitHub-Event", "X-GitHub-Delivery", "X-GitHub-Hook-ID", "User-Agent"} {
		if v := r.Header.Get(h); v != "" {
			headers[h] = v
		}
	}
	if err := CaptureBody(s.captureDir, source, name, headers, body); err != nil {
		s.log.Error("capture", "source", source, "error", err)
	}
}
