// Package alertmanager normalizes Alertmanager webhook deliveries (payload
// v4, verified against testdata/alertmanager/ captured from Alertmanager
// 0.33). Alerts are correlation anchors for `wtc around` — kind=alert, never
// part of diff/where.
package alertmanager

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
)

// payload is the webhook envelope; alert is one member of alerts[].
type payload struct {
	Status          string  `json:"status"`
	Alerts          []alert `json:"alerts"`
	TruncatedAlerts int     `json:"truncatedAlerts"`
}

type alert struct {
	Status       string            `json:"status"` // firing | resolved
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// EventFacts pairs a normalized event with its rule facts.
type EventFacts struct {
	Event *model.Event
	Facts normalize.Facts
}

// Normalize maps every alert in a delivery onto one Event per alert EPISODE:
// firing → started, resolved → succeeded, upserting the same
// am:<fingerprint>:<startsAt> row — so an episode is one row whose status
// closes when the alert resolves, with duration = endsAt - startsAt.
func Normalize(raw []byte, now time.Time) ([]EventFacts, error) {
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("decode alertmanager payload: %w", err)
	}

	var out []EventFacts
	for _, a := range p.Alerts {
		if a.Fingerprint == "" {
			return nil, fmt.Errorf("alert missing fingerprint (payload version?)")
		}
		status := model.StatusStarted
		ts := a.StartsAt
		var durationMS *int64
		if a.Status == "resolved" {
			status = model.StatusSucceeded
			if !a.EndsAt.IsZero() && a.EndsAt.Year() > 1 {
				ts = a.EndsAt
				d := a.EndsAt.Sub(a.StartsAt).Milliseconds()
				durationMS = &d
			}
		}

		name := a.Labels["alertname"]
		title := name
		if s := a.Annotations["summary"]; s != "" {
			title = fmt.Sprintf("%s: %s", name, s)
		}

		ev := &model.Event{
			ID:         model.NewID(),
			TS:         ts.UTC(),
			IngestedAt: now.UTC(),
			Source:     model.SourceAlertmanager,
			Kind:       model.KindAlert,
			Status:     status,
			Cluster:    a.Labels["cluster"],
			Namespace:  a.Labels["namespace"],
			Service:    a.Labels["service"],
			Actor:      "alertmanager",
			Title:      title,
			URL:        a.GeneratorURL,
			DurationMS: durationMS,
			DedupKey: fmt.Sprintf("am:%s:%s",
				a.Fingerprint, a.StartsAt.UTC().Format(time.RFC3339)),
		}
		facts := normalize.Facts{
			Source:     "alertmanager",
			Cluster:    a.Labels["cluster"],
			Namespace:  a.Labels["namespace"],
			ObjectName: name,
			Reason:     a.Labels["severity"],
		}
		out = append(out, EventFacts{Event: ev, Facts: facts})
	}
	return out, nil
}
