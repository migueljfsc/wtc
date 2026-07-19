package normalize

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/migueljfsc/wtc/internal/model"
)

// Backs `wtc explain`: which rule set each inferred field of an event. The
// engine matches ingest-time Facts, which are not reconstructible from stored
// payloads — so Apply persists them (plus the pre-rules field snapshot) on
// the event, and Explain replays the CURRENT rules over that record with a
// first-writer-wins trace.

// ruleFields are the event fields rules may set, in report order. Must stay
// in sync with the set-map in NewEngine and fieldEmpty/setField.
var ruleFields = []string{"env", "cluster", "namespace", "service", "kind", "actor"}

// factsRecord is the JSON stored in events.facts: the rule facts the parsers
// derived, plus the fields the normalizer had already set BEFORE rules ran —
// without the preset, a replay could not tell "normalizer set it" from "rule
// set it" under first-writer-wins.
type factsRecord struct {
	Facts  Facts             `json:"facts"`
	Preset map[string]string `json:"preset,omitempty"`
}

// EncodeFactsRecord serializes facts + preset for the events.facts column.
func EncodeFactsRecord(f Facts, preset map[string]string) string {
	b, err := json.Marshal(factsRecord{Facts: f, Preset: preset})
	if err != nil {
		return "" // cannot happen for these types; never block ingest over it
	}
	return string(b)
}

// DecodeFactsRecord parses an events.facts value.
func DecodeFactsRecord(s string) (Facts, map[string]string, error) {
	var r factsRecord
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return Facts{}, nil, fmt.Errorf("decode facts record: %w", err)
	}
	if r.Preset == nil {
		r.Preset = map[string]string{}
	}
	return r.Facts, r.Preset, nil
}

// presetFields snapshots the rule-settable fields the normalizer already
// filled, so a later Explain replays first-writer-wins faithfully.
func presetFields(ev *model.Event) map[string]string {
	m := map[string]string{}
	for _, field := range ruleFields {
		if !fieldEmpty(ev, field) {
			m[field] = FieldValue(ev, field)
		}
	}
	return m
}

// FieldValue reads a rule-settable field off an event by name. Exported for
// the explain endpoint's drift check (stored row vs current-rules replay).
func FieldValue(ev *model.Event, field string) string {
	switch field {
	case "env":
		return ev.Env
	case "cluster":
		return ev.Cluster
	case "namespace":
		return ev.Namespace
	case "service":
		return ev.Service
	case "kind":
		return string(ev.Kind)
	case "actor":
		return ev.Actor
	}
	return ""
}

// FieldTrace reports how one field got its value.
type FieldTrace struct {
	Field     string `json:"field"`
	Value     string `json:"value,omitempty"`
	Origin    string `json:"origin"` // "rule" | "normalizer" | "unmatched"
	RuleIndex *int   `json:"rule_index,omitempty"`
	RuleMatch string `json:"rule_match,omitempty"` // compact match spec of the winning rule
}

// Explain replays the rules over a recorded facts+preset pair and reports,
// per field, which rule set it (index + match spec), that the normalizer had
// pre-filled it (rules never ran for it), or that nothing matched. It runs
// the engine's CURRENT rules — "what would happen now" — which may differ
// from the ingest-time outcome after a rules edit; callers compare against
// the stored row to surface that.
func (e *Engine) Explain(f Facts, preset map[string]string) ([]FieldTrace, error) {
	shadow := &model.Event{}
	for field, v := range preset {
		setField(shadow, field, v)
	}

	byField := map[string]FieldTrace{}
	for i := range e.rules {
		r := &e.rules[i]
		if !r.matches(f) {
			continue
		}
		for field, tmpl := range r.set {
			if !fieldEmpty(shadow, field) {
				continue // first-writer-wins, as in Apply
			}
			var b strings.Builder
			if err := tmpl.Execute(&b, f); err != nil {
				return nil, fmt.Errorf("rule %d set %s: %w", i, field, err)
			}
			setField(shadow, field, b.String())
			if b.String() != "" {
				idx := i
				byField[field] = FieldTrace{
					Field: field, Value: b.String(), Origin: "rule",
					RuleIndex: &idx, RuleMatch: renderMatch(r.match),
				}
			}
		}
	}

	out := make([]FieldTrace, 0, len(ruleFields))
	for _, field := range ruleFields {
		if v := preset[field]; v != "" {
			out = append(out, FieldTrace{Field: field, Value: v, Origin: "normalizer"})
			continue
		}
		if t, ok := byField[field]; ok {
			out = append(out, t)
			continue
		}
		out = append(out, FieldTrace{Field: field, Origin: "unmatched"})
	}
	return out, nil
}

// Explain on the holder runs the current engine (DB overrides included).
func (h *EngineHolder) Explain(f Facts, preset map[string]string) ([]FieldTrace, error) {
	return h.p.Load().Explain(f, preset)
}

// renderMatch renders a rule's match block compactly for trace output, e.g.
// `source=flux namespace=prod-*`; an unconstrained match renders as `*`.
func renderMatch(m RuleMatch) string {
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	add("source", m.Source)
	add("repo", m.Repo)
	add("branch", m.Branch)
	add("event", m.Event)
	add("workflow", m.Workflow)
	add("cluster", m.Cluster)
	add("object_kind", m.ObjectKind)
	add("object_name", m.ObjectName)
	add("namespace", m.Namespace)
	if len(m.Paths) > 0 {
		parts = append(parts, "paths=["+strings.Join(m.Paths, ", ")+"]")
	}
	if len(parts) == 0 {
		return "*"
	}
	return strings.Join(parts, " ")
}
