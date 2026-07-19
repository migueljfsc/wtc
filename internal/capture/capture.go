// Package capture writes raw ingest payloads to disk for the fixture-first
// workflow. It is a leaf dependency shared by the HTTP server and
// the API pollers, so ingest packages can capture without importing server
// (which would form an import cycle once server routes to those packages).
package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// Body writes one raw payload under dir/<source>/. name should carry the
// source-side identity (delivery id, repo+resource) — it is sanitized and
// timestamped. Errors are returned, not fatal: capture must never break ingest.
func Body(dir, source, name string, headers map[string]string, body []byte) error {
	sub := filepath.Join(dir, source)
	if err := os.MkdirAll(sub, 0o750); err != nil {
		return fmt.Errorf("capture mkdir: %w", err)
	}
	base := time.Now().UTC().Format("20060102T150405.000Z") + "-" + SanitizeFilename(name)

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

// SanitizeFilename maps a name to a filesystem-safe token; empty names get a
// generated ULID so a capture is never lost to a missing identity.
func SanitizeFilename(s string) string {
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
