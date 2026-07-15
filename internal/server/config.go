package server

import (
	"net/http"

	"github.com/migueljfsc/wtc/internal/normalize"
)

// ConfigResponse is the read-only view of the normalization config the engine
// is currently using (rules + tag patterns). Secrets never appear here — rules
// and tag_patterns hold only globs/regexes and field templates.
type ConfigResponse struct {
	Rules       []normalize.Rule `json:"rules"`
	TagPatterns []string         `json:"tag_patterns"`
}

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	rules := s.rules
	if rules == nil {
		rules = []normalize.Rule{}
	}
	tags := s.tagPatterns
	if tags == nil {
		tags = []string{}
	}
	s.writeJSON(w, http.StatusOK, ConfigResponse{Rules: rules, TagPatterns: tags})
}
