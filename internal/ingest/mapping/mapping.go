// Package mapping implements the config-declared "mapping webhook" (P14):
// POST /ingest/webhook/<name> where an operator turns any tool that emits JSON
// into a wtc source through configuration, not code. A Webhook declares auth, a
// payload→Event field mapping (go-templates over the parsed JSON body — the
// same template engine `rules[].set` uses), a stable dedup_key template, and
// optional facts feeding the rules engine. Mapped events enter the standard
// pipeline (rules → redaction → status-rank upsert), so lifecycle transitions
// work when a sender emits phase updates.
//
// One delivery maps to one Event: the long-tail case is a single JSON body
// describing a single change. Array fan-out (à la alertmanager) is out of
// scope — a tool that batches is served by its own parser, not this path.
package mapping

import (
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
)

// Webhook is one operator-declared mapping source (config `sources.webhooks[]`).
// Compiled into a *Mapper at startup so template/config errors fail the daemon
// rather than every delivery.
type Webhook struct {
	Name     string            `yaml:"name"`
	Preset   string            `yaml:"preset"`    // load a shipped preset as the base
	Auth     Auth              `yaml:"auth"`      // static token XOR hmac — one is required
	DedupKey string            `yaml:"dedup_key"` // REQUIRED go-template; the idempotency footgun
	Mapping  FieldTemplates    `yaml:"mapping"`   // payload → Event field templates
	Facts    map[string]string `yaml:"facts"`     // fact name → template, feeds the rules engine
}

// Auth is a mapping webhook's authentication. Exactly one of Token (a static
// shared secret sent verbatim in a header) or HMAC (the sender signs the body)
// must be configured; both empty is a startup error (fail closed). The two
// shapes mirror the existing paths: static-token like argocd/gitlab, HMAC like
// github/flux.
type Auth struct {
	Token  string    `yaml:"token"`  // static shared secret
	Header string    `yaml:"header"` // header carrying Token; default X-WTC-Token
	HMAC   *HMACAuth `yaml:"hmac"`
}

// HMACAuth verifies a sender-computed HMAC over the raw body.
type HMACAuth struct {
	Secret string `yaml:"secret"`
	Header string `yaml:"header"` // header carrying the signature, e.g. X-Hub-Signature-256
	Algo   string `yaml:"algo"`   // sha256 (default) | sha512 | sha1
	Prefix string `yaml:"prefix"` // stripped from the header value, e.g. "sha256="
}

// FieldTemplates are the per-Event-field go-templates. Empty leaves the field
// unset — for env/cluster/namespace/service the rules engine may then fill it
// from Facts (first-writer-wins). Kind is required (Event.Validate enforces a
// valid kind); Status defaults to unknown.
type FieldTemplates struct {
	Kind       string `yaml:"kind"`
	Status     string `yaml:"status"`
	Env        string `yaml:"env"`
	Cluster    string `yaml:"cluster"`
	Namespace  string `yaml:"namespace"`
	Service    string `yaml:"service"`
	Actor      string `yaml:"actor"`
	Ref        string `yaml:"ref"`
	Artifact   string `yaml:"artifact"`
	Title      string `yaml:"title"`
	URL        string `yaml:"url"`
	TS         string `yaml:"ts"`          // RFC3339; default = ingest time
	DurationMS string `yaml:"duration_ms"` // integer milliseconds
}

// Mapper is a compiled Webhook.
type Mapper struct {
	name   string
	auth   Auth
	fields map[string]*template.Template // event field name → template
	dedup  *template.Template
	facts  map[string]*template.Template // fact name → template
}

// Name returns the source name (also the stored Event.Source).
func (m *Mapper) Name() string { return m.name }

// AuthConfig exposes the compiled auth so the server can verify a request.
func (m *Mapper) AuthConfig() Auth { return m.auth }

// EventFacts pairs a normalized event with its rule facts, matching the shape
// the other ingest packages hand to the server pipeline.
type EventFacts struct {
	Event *model.Event
	Facts normalize.Facts
}

// tmplFuncs reuses the rules-engine funcs and adds `default` for the common
// "fall back when a payload field is absent" case.
var tmplFuncs = func() template.FuncMap {
	fm := template.FuncMap{
		"default": func(def string, v any) string {
			if v == nil || v == "" {
				return def
			}
			return fmt.Sprint(v)
		},
	}
	for k, v := range normalize.TemplateFuncs() {
		fm[k] = v
	}
	return fm
}()

// validFactKeys are the Facts fields a mapping may populate. Scalar only; the
// path-glob rules (Facts.Paths) are not reachable from a flat template and are
// intentionally unsupported here.
var factSetters = map[string]func(*normalize.Facts, string){
	"repo":        func(f *normalize.Facts, v string) { f.Repo = v },
	"branch":      func(f *normalize.Facts, v string) { f.Branch = v },
	"event":       func(f *normalize.Facts, v string) { f.Event = v },
	"workflow":    func(f *normalize.Facts, v string) { f.Workflow = v },
	"actor":       func(f *normalize.Facts, v string) { f.Actor = v },
	"cluster":     func(f *normalize.Facts, v string) { f.Cluster = v },
	"namespace":   func(f *normalize.Facts, v string) { f.Namespace = v },
	"object_kind": func(f *normalize.Facts, v string) { f.ObjectKind = v },
	"object_name": func(f *normalize.Facts, v string) { f.ObjectName = v },
	"reason":      func(f *normalize.Facts, v string) { f.Reason = v },
}

// Compile resolves presets, validates, and compiles the declared webhooks into
// Mappers keyed by name. Every failure is a startup error — a misconfigured
// mapping must never limp into serving and silently drop deliveries.
func Compile(webhooks []Webhook) (map[string]*Mapper, error) {
	out := map[string]*Mapper{}
	for i, w := range webhooks {
		if w.Preset != "" {
			base, ok := Preset(w.Preset)
			if !ok {
				return nil, fmt.Errorf("webhooks[%d] (%s): unknown preset %q", i, w.Name, w.Preset)
			}
			w = merge(base, w)
		}
		if w.Name == "" {
			return nil, fmt.Errorf("webhooks[%d]: name is required", i)
		}
		if model.BuiltinSource(model.Source(w.Name)) {
			return nil, fmt.Errorf("webhooks[%d]: name %q collides with a built-in source", i, w.Name)
		}
		if _, dup := out[w.Name]; dup {
			return nil, fmt.Errorf("webhooks[%d]: duplicate name %q", i, w.Name)
		}
		if err := validateAuth(w.Auth); err != nil {
			return nil, fmt.Errorf("webhooks[%d] (%s): %w", i, w.Name, err)
		}
		if w.DedupKey == "" {
			return nil, fmt.Errorf("webhooks[%d] (%s): dedup_key template is required (an unstable key breaks idempotency)", i, w.Name)
		}

		m := &Mapper{name: w.Name, auth: w.Auth, fields: map[string]*template.Template{}, facts: map[string]*template.Template{}}
		var err error
		if m.dedup, err = parse(w.Name+".dedup_key", w.DedupKey); err != nil {
			return nil, err
		}
		for field, expr := range map[string]string{
			"kind": w.Mapping.Kind, "status": w.Mapping.Status, "env": w.Mapping.Env,
			"cluster": w.Mapping.Cluster, "namespace": w.Mapping.Namespace, "service": w.Mapping.Service,
			"actor": w.Mapping.Actor, "ref": w.Mapping.Ref, "artifact": w.Mapping.Artifact,
			"title": w.Mapping.Title, "url": w.Mapping.URL, "ts": w.Mapping.TS, "duration_ms": w.Mapping.DurationMS,
		} {
			if expr == "" {
				continue
			}
			t, perr := parse(w.Name+".mapping."+field, expr)
			if perr != nil {
				return nil, perr
			}
			m.fields[field] = t
		}
		if w.Mapping.Kind == "" {
			return nil, fmt.Errorf("webhooks[%d] (%s): mapping.kind is required", i, w.Name)
		}
		if w.Mapping.Title == "" {
			return nil, fmt.Errorf("webhooks[%d] (%s): mapping.title is required", i, w.Name)
		}
		for fact, expr := range w.Facts {
			if _, ok := factSetters[fact]; !ok {
				return nil, fmt.Errorf("webhooks[%d] (%s): unknown fact %q", i, w.Name, fact)
			}
			t, perr := parse(w.Name+".facts."+fact, expr)
			if perr != nil {
				return nil, perr
			}
			m.facts[fact] = t
		}
		out[w.Name] = m
	}
	return out, nil
}

func validateAuth(a Auth) error {
	hasToken := a.Token != ""
	hasHMAC := a.HMAC != nil && a.HMAC.Secret != ""
	switch {
	case hasToken && hasHMAC:
		return fmt.Errorf("auth: set exactly one of token or hmac, not both")
	case !hasToken && !hasHMAC:
		return fmt.Errorf("auth: a static token or hmac secret is required (fail closed)")
	}
	if hasHMAC {
		switch a.HMAC.Algo {
		case "", "sha256", "sha512", "sha1":
		default:
			return fmt.Errorf("auth.hmac.algo %q unsupported (sha256|sha512|sha1)", a.HMAC.Algo)
		}
		if a.HMAC.Header == "" {
			return fmt.Errorf("auth.hmac.header is required")
		}
	}
	return nil
}

func parse(name, expr string) (*template.Template, error) {
	t, err := template.New(name).Funcs(tmplFuncs).Option("missingkey=zero").Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	return t, nil
}

// Normalize renders a delivery body into one EventFacts. root is the parsed
// JSON body. A template evaluation error, an empty rendered dedup_key, or an
// empty title are returned as errors so the server surfaces them (doctor) and
// never guesses — an unstable or empty key silently breaks idempotency.
func (m *Mapper) Normalize(root any, now time.Time) (EventFacts, error) {
	render := func(field string, t *template.Template) (string, error) {
		var b strings.Builder
		if err := t.Execute(&b, root); err != nil {
			return "", fmt.Errorf("mapping %s: eval %s: %w", m.name, field, err)
		}
		// missingkey=zero renders an absent key as the literal "<no value>";
		// strip it so an optional field (e.g. an absent env label) becomes ""
		// rather than storing the token — and a required field that renders only
		// "<no value>" then trips the empty-dedup_key/empty-title guards.
		return strings.TrimSpace(strings.ReplaceAll(b.String(), "<no value>", "")), nil
	}

	dedup, err := render("dedup_key", m.dedup)
	if err != nil {
		return EventFacts{}, err
	}
	if dedup == "" {
		return EventFacts{}, fmt.Errorf("mapping %s: dedup_key rendered empty (unstable key breaks idempotency)", m.name)
	}

	vals := map[string]string{}
	for field, t := range m.fields {
		v, verr := render(field, t)
		if verr != nil {
			return EventFacts{}, verr
		}
		vals[field] = v
	}
	if vals["title"] == "" {
		return EventFacts{}, fmt.Errorf("mapping %s: title rendered empty", m.name)
	}

	ev := &model.Event{
		ID:         model.NewID(),
		IngestedAt: now.UTC(),
		Source:     model.Source(m.name),
		Kind:       model.Kind(vals["kind"]),
		Status:     model.Status(vals["status"]),
		Env:        vals["env"],
		Cluster:    vals["cluster"],
		Namespace:  vals["namespace"],
		Service:    vals["service"],
		Actor:      vals["actor"],
		Ref:        vals["ref"],
		Artifact:   vals["artifact"],
		Title:      vals["title"],
		URL:        vals["url"],
		DedupKey:   dedup,
	}
	if ev.Status == "" {
		ev.Status = model.StatusUnknown
	}
	if ts := vals["ts"]; ts != "" {
		parsed, terr := model.ParseTS(ts)
		if terr != nil {
			return EventFacts{}, fmt.Errorf("mapping %s: ts %q: %w", m.name, ts, terr)
		}
		ev.TS = parsed
	} else {
		ev.TS = now.UTC()
	}
	if d := vals["duration_ms"]; d != "" {
		ms, cerr := strconv.ParseInt(d, 10, 64)
		if cerr != nil {
			return EventFacts{}, fmt.Errorf("mapping %s: duration_ms %q: %w", m.name, d, cerr)
		}
		ev.DurationMS = &ms
	}

	facts := normalize.Facts{Source: m.name}
	for fact, t := range m.facts {
		v, verr := render("facts."+fact, t)
		if verr != nil {
			return EventFacts{}, verr
		}
		factSetters[fact](&facts, v)
	}

	return EventFacts{Event: ev, Facts: facts}, nil
}
