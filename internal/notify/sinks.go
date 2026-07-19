package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/migueljfsc/wtc/internal/model"
)

// WebhookPayload is the wire shape of a generic webhook delivery. Event is
// the post-merge ledger row with payload/facts omitted — receivers wanting
// the raw body fetch /api/events with the id.
type WebhookPayload struct {
	Notification string      `json:"notification"`
	Transition   bool        `json:"transition"` // true = status change on an existing row
	Event        model.Event `json:"event"`
}

func (d *Dispatcher) sendWebhook(ctx context.Context, dl delivery) error {
	body, err := json.Marshal(WebhookPayload{
		Notification: dl.sub.name,
		Transition:   dl.transitioned,
		Event:        dl.ev,
	})
	if err != nil {
		return fmt.Errorf("encode webhook payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dl.sub.sink.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if dl.sub.sink.Token != "" {
		req.Header.Set("Authorization", "Bearer "+dl.sub.sink.Token)
	}
	return d.do(req, "webhook")
}

// grafanaAnnotation is the Grafana POST /api/annotations request body.
// Shape verified against a live Grafana round-trip — see
// testdata/grafana/annotation-*.json (P21 fixture discipline).
type grafanaAnnotation struct {
	Time int64    `json:"time"` // epoch millis
	Tags []string `json:"tags"`
	Text string   `json:"text"`
}

// sendGrafanaAnnotation posts the event as a dashboard annotation so "what
// changed" overlays Grafana graphs. Tags: "wtc", the kind, "env:<env>" when
// mapped, plus any configured extras.
func (d *Dispatcher) sendGrafanaAnnotation(ctx context.Context, dl delivery) error {
	ev := dl.ev
	tags := []string{"wtc", string(ev.Kind)}
	if ev.Env != "" {
		tags = append(tags, "env:"+ev.Env)
	}
	if ev.Service != "" {
		tags = append(tags, "service:"+ev.Service)
	}
	tags = append(tags, dl.sub.sink.Tags...)

	text := fmt.Sprintf("%s: %s", ev.Status, ev.Title)
	if ev.URL != "" {
		// Grafana renders sanitized HTML in annotation text; a plain <a> link
		// survives and gives click-through to the source system.
		text += fmt.Sprintf(`<br/><a href=%q>details</a>`, ev.URL)
	}

	body, err := json.Marshal(grafanaAnnotation{Time: ev.TS.UnixMilli(), Tags: tags, Text: text})
	if err != nil {
		return fmt.Errorf("encode grafana annotation: %w", err)
	}
	url := strings.TrimSuffix(dl.sub.sink.URL, "/") + "/api/annotations"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build grafana request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+dl.sub.sink.Token)
	return d.do(req, "grafana")
}

// do executes an outbound sink request and maps non-2xx to an error carrying
// a bounded body snippet. Never logs or returns the URL — sink URLs are
// secrets (Slack webhook URLs are capability-bearing).
func (d *Dispatcher) do(req *http.Request, sink string) error {
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("post to %s: %w", sink, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s rejected the notification: HTTP %d: %s", sink, resp.StatusCode, snippet)
	}
	return nil
}

// eventSlackText renders one event as a Slack mrkdwn line, structurally
// consistent with the digest's bullet style.
func eventSlackText(ev model.Event, transitioned bool) string {
	env := ev.Env
	if env == "" {
		env = "(unmapped)"
	}
	subject := ev.Service
	if subject == "" {
		subject = ev.Repo
	}
	if subject != "" {
		subject = " " + slackEscape(subject)
	}
	title := slackEscape(ev.Title)
	if ev.URL != "" {
		title = fmt.Sprintf("<%s|%s>", ev.URL, title)
	}
	verb := ""
	if transitioned {
		verb = " →" // status changed on an existing row vs first sighting
	}
	return fmt.Sprintf("• *[%s]* %s%s%s *%s* — %s",
		slackEscape(env), ev.Kind, subject, verb, ev.Status, title)
}

// slackEscape escapes the three characters Slack's mrkdwn treats as control
// characters in text (per api.slack.com/reference/surfaces/formatting).
func slackEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}
