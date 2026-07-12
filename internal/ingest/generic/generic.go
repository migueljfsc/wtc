// Package generic implements the /ingest/generic schema shared by the HTTP
// endpoint and the `wtc record` client.
package generic

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// Request is the JSON body accepted by POST /ingest/generic — an Event
// subset; the server fills id, ingested_at, and defaults.
type Request struct {
	Source     string   `json:"source,omitempty"` // default "generic"
	Kind       string   `json:"kind"`
	Status     string   `json:"status,omitempty"` // default "unknown"
	Env        string   `json:"env,omitempty"`
	Cluster    string   `json:"cluster,omitempty"`
	Namespace  string   `json:"namespace,omitempty"`
	Service    string   `json:"service,omitempty"`
	Actor      string   `json:"actor,omitempty"`
	Ref        string   `json:"ref,omitempty"`
	Artifact   string   `json:"artifact,omitempty"`
	Artifacts  []string `json:"artifacts,omitempty"` // stored in payload
	Title      string   `json:"title"`
	URL        string   `json:"url,omitempty"`
	TS         string   `json:"ts,omitempty"` // RFC3339; default now
	DurationMS *int64   `json:"duration_ms,omitempty"`
	DedupKey   string   `json:"dedup_key,omitempty"` // default "generic:<id>"
}

// ToEvent converts the request into a validated Event. now is used for
// ingested_at and as the ts fallback.
func (r *Request) ToEvent(now time.Time) (*model.Event, error) {
	ev := &model.Event{
		ID:         model.NewID(),
		IngestedAt: now.UTC(),
		Source:     model.Source(r.Source),
		Kind:       model.Kind(r.Kind),
		Status:     model.Status(r.Status),
		Env:        r.Env,
		Cluster:    r.Cluster,
		Namespace:  r.Namespace,
		Service:    r.Service,
		Actor:      r.Actor,
		Ref:        r.Ref,
		Artifact:   r.Artifact,
		Title:      r.Title,
		URL:        r.URL,
		DurationMS: r.DurationMS,
		DedupKey:   r.DedupKey,
	}

	if ev.Source == "" {
		ev.Source = model.SourceGeneric
	}
	if ev.Status == "" {
		ev.Status = model.StatusUnknown
	}
	if ev.DedupKey == "" {
		ev.DedupKey = "generic:" + ev.ID
	}

	if r.TS == "" {
		ev.TS = now.UTC()
	} else {
		ts, err := model.ParseTS(r.TS)
		if err != nil {
			return nil, err
		}
		ev.TS = ts
	}

	if len(r.Artifacts) > 0 {
		payload, err := json.Marshal(map[string]any{"artifacts": r.Artifacts})
		if err != nil {
			return nil, fmt.Errorf("encode artifacts payload: %w", err)
		}
		ev.Payload = string(payload)
	}

	if err := ev.Validate(); err != nil {
		return nil, err
	}
	return ev, nil
}
