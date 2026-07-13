// Package normalize hosts the normalization pipeline. Phase 0 ships the
// redaction pass (CLAUDE.md hard decision: redaction before storage); the
// env/service/cluster rules engine lands in Phase 1.
package normalize

import (
	"regexp"

	"github.com/migueljfsc/wtc/internal/model"
)

const redacted = "[REDACTED]"

// Regex deny-list per CLAUDE.md: AWS keys, GitHub tokens, bearer tokens,
// password|secret|token key/value pairs. Order matters only for readability.
var (
	simplePatterns = []*regexp.Regexp{
		regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),                  // AWS access key id
		regexp.MustCompile(`\bghp_[A-Za-z0-9]{20,}\b`),              // GitHub classic PAT
		regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),      // GitHub fine-grained PAT
		regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`), // Authorization header values
	}
	// key[:=]value forms; keeps the key so operators can see what was cut.
	kvPattern = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api_key|apikey)\b["']?\s*[:=]\s*["']?[^\s"']+`)
)

// Redact scrubs secrets from s per the deny-list.
func Redact(s string) string {
	if s == "" {
		return s
	}
	for _, p := range simplePatterns {
		s = p.ReplaceAllString(s, redacted)
	}
	return kvPattern.ReplaceAllString(s, "$1="+redacted)
}

// RedactEvent scrubs the fields that can carry free-form or raw-payload text.
// Must run before Store.Ingest on every ingest path.
func RedactEvent(ev *model.Event) {
	ev.Title = Redact(ev.Title)
	ev.Payload = Redact(ev.Payload)
}
