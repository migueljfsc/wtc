package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/migueljfsc/wtc/internal/config"
	"github.com/migueljfsc/wtc/internal/normalize"
)

const (
	cfgKeyRules = "rules"
	cfgKeyTags  = "tag_patterns"
)

// ConfigResponse is the effective config surface: the live-editable
// normalization parts (rules/tag_patterns + override flags) plus the
// redacted static snapshot of everything else (config.View). Secrets
// never appear here — the view masks them as a constant "********" and the
// sentinel guard test in internal/config proves it.
type ConfigResponse struct {
	config.View
	Rules                 []normalize.Rule `json:"rules"`
	TagPatterns           []string         `json:"tag_patterns"`
	RulesOverridden       bool             `json:"rules_overridden"`
	TagPatternsOverridden bool             `json:"tag_patterns_overridden"`
}

// loadConfigOverrides applies any DB-stored rules/tag_patterns over the YAML
// baseline at startup, swapping the holders before ingest begins. Best-effort:
// a bad or absent override leaves the (already-installed) baseline in place.
func (s *Server) loadConfigOverrides() {
	ctx := context.Background()

	if raw, ok, err := s.store.GetConfigOverride(ctx, cfgKeyRules); err != nil {
		s.log.Error("load rules override", "error", err)
	} else if ok {
		var rules []normalize.Rule
		if err := json.Unmarshal([]byte(raw), &rules); err != nil {
			s.log.Error("parse rules override", "error", err)
		} else if eng, err := normalize.NewEngine(rules, normalize.WithOwnerResolver(s.ownerResolver)); err != nil {
			s.log.Error("compile rules override", "error", err)
		} else {
			s.engine.Swap(eng)
			s.curRules, s.rulesFromDB = rules, true
			s.log.Info("rules loaded from DB override", "count", len(rules))
		}
	}

	if raw, ok, err := s.store.GetConfigOverride(ctx, cfgKeyTags); err != nil {
		s.log.Error("load tag_patterns override", "error", err)
	} else if ok {
		var pats []string
		if err := json.Unmarshal([]byte(raw), &pats); err != nil {
			s.log.Error("parse tag_patterns override", "error", err)
		} else if res, err := normalize.NewTagResolver(pats); err != nil {
			s.log.Error("compile tag_patterns override", "error", err)
		} else {
			s.tags.Swap(res)
			s.curTags, s.tagsFromDB = pats, true
			s.log.Info("tag_patterns loaded from DB override", "count", len(pats))
		}
	}
}

func (s *Server) snapshotConfig() ConfigResponse {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	rules := s.curRules
	if rules == nil {
		rules = []normalize.Rule{}
	}
	tags := s.curTags
	if tags == nil {
		tags = []string{}
	}
	return ConfigResponse{
		View:                  s.configView,
		Rules:                 rules,
		TagPatterns:           tags,
		RulesOverridden:       s.rulesFromDB,
		TagPatternsOverridden: s.tagsFromDB,
	}
}

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, s.snapshotConfig())
}

// handlePutRules validates, persists and hot-reloads a new rule set. A rule
// that fails to compile (bad glob/template) is rejected with 400 and nothing
// changes.
func (s *Server) handlePutRules(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rules []normalize.Rule `json:"rules"`
	}
	if !s.decodeBody(w, r, &body) {
		return
	}
	eng, err := normalize.NewEngine(body.Rules, normalize.WithOwnerResolver(s.ownerResolver))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid rules: "+err.Error())
		return
	}
	raw, err := json.Marshal(body.Rules)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "encode rules")
		return
	}
	if err := s.store.SetConfigOverride(r.Context(), cfgKeyRules, string(raw)); err != nil {
		s.log.Error("persist rules override", "error", err)
		s.writeError(w, http.StatusInternalServerError, "storage error")
		return
	}
	s.cfgMu.Lock()
	s.engine.Swap(eng)
	s.curRules, s.rulesFromDB = body.Rules, true
	s.cfgMu.Unlock()
	s.writeJSON(w, http.StatusOK, s.snapshotConfig())
}

// handleResetRules drops the override and reloads the YAML baseline.
func (s *Server) handleResetRules(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteConfigOverride(r.Context(), cfgKeyRules); err != nil {
		s.log.Error("reset rules override", "error", err)
		s.writeError(w, http.StatusInternalServerError, "storage error")
		return
	}
	eng, err := normalize.NewEngine(s.fileRules, normalize.WithOwnerResolver(s.ownerResolver)) // valid at startup
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "rebuild baseline rules")
		return
	}
	s.cfgMu.Lock()
	s.engine.Swap(eng)
	s.curRules, s.rulesFromDB = s.fileRules, false
	s.cfgMu.Unlock()
	s.writeJSON(w, http.StatusOK, s.snapshotConfig())
}

func (s *Server) handlePutTagPatterns(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TagPatterns []string `json:"tag_patterns"`
	}
	if !s.decodeBody(w, r, &body) {
		return
	}
	res, err := normalize.NewTagResolver(body.TagPatterns)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid tag_patterns: "+err.Error())
		return
	}
	raw, err := json.Marshal(body.TagPatterns)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "encode tag_patterns")
		return
	}
	if err := s.store.SetConfigOverride(r.Context(), cfgKeyTags, string(raw)); err != nil {
		s.log.Error("persist tag_patterns override", "error", err)
		s.writeError(w, http.StatusInternalServerError, "storage error")
		return
	}
	s.cfgMu.Lock()
	s.tags.Swap(res)
	s.curTags, s.tagsFromDB = body.TagPatterns, true
	s.cfgMu.Unlock()
	s.writeJSON(w, http.StatusOK, s.snapshotConfig())
}

func (s *Server) handleResetTagPatterns(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteConfigOverride(r.Context(), cfgKeyTags); err != nil {
		s.log.Error("reset tag_patterns override", "error", err)
		s.writeError(w, http.StatusInternalServerError, "storage error")
		return
	}
	res, err := normalize.NewTagResolver(s.fileTags)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "rebuild baseline tag_patterns")
		return
	}
	s.cfgMu.Lock()
	s.tags.Swap(res)
	s.curTags, s.tagsFromDB = s.fileTags, false
	s.cfgMu.Unlock()
	s.writeJSON(w, http.StatusOK, s.snapshotConfig())
}

// decodeBody reads a bounded JSON request body into v, writing a 400 on
// failure. Returns false when the caller should stop.
func (s *Server) decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(body).Decode(v); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}
