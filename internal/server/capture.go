package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// Capture mode (CLAUDE.md fixture-first workflow): when a capture directory
// is configured, every raw ingest body plus its headers is dumped to disk so
// real payloads can be frozen into testdata/ fixtures. Dev-only by design.

// CaptureBody writes one raw payload under dir/<source>/. name should carry
// the source-side identity (delivery id, repo+resource) — it is sanitized and
// timestamped. Errors are returned, not fatal: capture must never break ingest.
func CaptureBody(dir, source, name string, headers map[string]string, body []byte) error {
	sub := filepath.Join(dir, source)
	if err := os.MkdirAll(sub, 0o750); err != nil {
		return fmt.Errorf("capture mkdir: %w", err)
	}
	base := time.Now().UTC().Format("20060102T150405.000Z") + "-" + sanitizeFilename(name)

	if err := os.WriteFile(filepath.Join(sub, base+".json"), body, 0o600); err != nil {
		return fmt.Errorf("capture body: %w", err)
	}
	if len(headers) > 0 {
		var b strings.Builder
		for k, v := range headers {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
		if err := os.WriteFile(filepath.Join(sub, base+".headers"), []byte(b.String()), 0o600); err != nil {
			return fmt.Errorf("capture headers: %w", err)
		}
	}
	return nil
}

func sanitizeFilename(s string) string {
	if s == "" {
		return model.NewID()
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, s)
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
