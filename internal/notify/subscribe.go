package notify

import (
	"fmt"
	"regexp"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
)

// Subscription is one config `notifications:` entry: a glob match over
// stored-event fields plus the sink deliveries go to. The schema lives here
// (not in config), mirroring the mapping.Webhook pattern — config embeds
// it and validates via Compile at load.
type Subscription struct {
	// Name identifies the entry in logs and the wtc_notify_* metric labels.
	// Empty defaults to "notifications[<index>]".
	Name  string `yaml:"name"`
	Match Match  `yaml:"match"`
	Sink  Sink   `yaml:"sink"`
}

// Match selects events with the rules-engine glob dialect (* one segment,
// ** any depth; normalize.CompileGlob). Empty fields are unconstrained; an
// entirely empty match subscribes to everything. Matching runs against the
// POST-MERGE row, so a lifecycle completion that omits env still matches an
// `env: prod` subscription via the value the ledger kept.
type Match struct {
	Env     string `yaml:"env" json:"env,omitempty"`
	Service string `yaml:"service" json:"service,omitempty"`
	Repo    string `yaml:"repo" json:"repo,omitempty"`
	Kind    string `yaml:"kind" json:"kind,omitempty"`
	Status  string `yaml:"status" json:"status,omitempty"`
}

// Sink is where matched events are delivered. Exactly one behavior per Type;
// unused fields must stay empty (Compile rejects a shape that doesn't fit the
// type, so a typo'd key fails startup instead of being silently ignored).
type Sink struct {
	// Type is "slack", "webhook", or "grafana-annotation".
	Type string `yaml:"type"`
	// URL is the delivery endpoint: the Slack incoming-webhook URL, the
	// generic webhook endpoint, or the Grafana base URL (scheme://host[:port],
	// the sink appends /api/annotations). Secret for slack (capability-bearing),
	// so always masked in the config view.
	URL string `yaml:"url"`
	// Token is sent as an Authorization: Bearer header. Required for
	// grafana-annotation (service-account token), optional for webhook.
	Token string `yaml:"token"`
	// Tags are extra Grafana annotation tags, appended to the defaults
	// ("wtc", kind, env). grafana-annotation only.
	Tags []string `yaml:"tags"`
}

// Sink type names.
const (
	SinkSlack   = "slack"
	SinkWebhook = "webhook"
	SinkGrafana = "grafana-annotation"
)

type compiledSub struct {
	name                             string
	env, service, repo, kind, status *regexp.Regexp // nil = unconstrained
	sink                             Sink
}

// Compiled is a validated, glob-compiled subscription set ready for the
// Dispatcher. Immutable after Compile.
type Compiled struct {
	subs []compiledSub
}

// Empty reports whether no subscriptions are configured.
func (c *Compiled) Empty() bool { return c == nil || len(c.subs) == 0 }

// Compile validates subscriptions and compiles their globs up front so a bad
// pattern or sink shape fails startup, never a delivery.
func Compile(subs []Subscription) (*Compiled, error) {
	c := &Compiled{}
	for i, sub := range subs {
		cs := compiledSub{name: sub.Name, sink: sub.Sink}
		if cs.name == "" {
			cs.name = fmt.Sprintf("notifications[%d]", i)
		}

		var err error
		compile := func(dst **regexp.Regexp, pattern string) {
			if err != nil || pattern == "" {
				return
			}
			*dst, err = normalize.CompileGlob(pattern)
		}
		compile(&cs.env, sub.Match.Env)
		compile(&cs.service, sub.Match.Service)
		compile(&cs.repo, sub.Match.Repo)
		compile(&cs.kind, sub.Match.Kind)
		compile(&cs.status, sub.Match.Status)
		if err != nil {
			return nil, fmt.Errorf("notifications[%d] (%s): %w", i, cs.name, err)
		}

		if err := validateSink(sub.Sink); err != nil {
			return nil, fmt.Errorf("notifications[%d] (%s): %w", i, cs.name, err)
		}
		c.subs = append(c.subs, cs)
	}
	return c, nil
}

func validateSink(s Sink) error {
	switch s.Type {
	case SinkSlack:
		if s.URL == "" {
			return fmt.Errorf("sink slack: url (incoming-webhook URL) is required")
		}
		if s.Token != "" || len(s.Tags) > 0 {
			return fmt.Errorf("sink slack: token/tags do not apply")
		}
	case SinkWebhook:
		if s.URL == "" {
			return fmt.Errorf("sink webhook: url is required")
		}
		if len(s.Tags) > 0 {
			return fmt.Errorf("sink webhook: tags do not apply")
		}
	case SinkGrafana:
		if s.URL == "" || s.Token == "" {
			return fmt.Errorf("sink grafana-annotation: url and token are required")
		}
	case "":
		return fmt.Errorf("sink: type is required (slack | webhook | grafana-annotation)")
	default:
		return fmt.Errorf("sink: unknown type %q (want slack | webhook | grafana-annotation)", s.Type)
	}
	return nil
}

func (cs *compiledSub) matches(ev model.Event) bool {
	check := func(re *regexp.Regexp, val string) bool { return re == nil || re.MatchString(val) }
	return check(cs.env, ev.Env) && check(cs.service, ev.Service) &&
		check(cs.repo, ev.Repo) && check(cs.kind, string(ev.Kind)) &&
		check(cs.status, string(ev.Status))
}
