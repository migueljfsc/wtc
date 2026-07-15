package normalize

import (
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/migueljfsc/wtc/internal/model"
)

// Facts is the per-event fact map the rules engine matches against and
// templates render from (SPEC §3). Parsers fill what they know; empty means
// unknown.
type Facts struct {
	Source     string
	Repo       string
	Branch     string
	Event      string
	Workflow   string // CI workflow name — the service signal in monorepos
	Actor      string
	Cluster    string
	ObjectKind string
	ObjectName string
	Namespace  string
	Reason     string
	Paths      []string
	// PathsTruncated marks an unknown/truncated changed-file list (GitHub
	// caps push payloads; list APIs omit files). Path-based rules are then
	// SKIPPED — never treated as "no match" (CLAUDE.md trap #3).
	PathsTruncated bool
}

// RuleMatch selects events. Empty fields are unconstrained; strings support
// * and ** globs; Paths matches when ANY changed path matches ANY pattern.
type RuleMatch struct {
	Source     string   `yaml:"source" json:"source,omitempty"`
	Repo       string   `yaml:"repo" json:"repo,omitempty"`
	Branch     string   `yaml:"branch" json:"branch,omitempty"`
	Event      string   `yaml:"event" json:"event,omitempty"`
	Workflow   string   `yaml:"workflow" json:"workflow,omitempty"`
	Cluster    string   `yaml:"cluster" json:"cluster,omitempty"`
	ObjectKind string   `yaml:"object_kind" json:"object_kind,omitempty"`
	Paths      []string `yaml:"paths" json:"paths,omitempty"`
}

// RuleSet holds the fields a rule may set. Values are Go templates over the
// fact map with funcs trimPrefix, trimSuffix, lower, regexReplace.
type RuleSet struct {
	Env       string `yaml:"env" json:"env,omitempty"`
	Cluster   string `yaml:"cluster" json:"cluster,omitempty"`
	Namespace string `yaml:"namespace" json:"namespace,omitempty"`
	Service   string `yaml:"service" json:"service,omitempty"`
	Kind      string `yaml:"kind" json:"kind,omitempty"`
	Actor     string `yaml:"actor" json:"actor,omitempty"`
}

// Rule is one ordered entry of the config `rules:` list.
type Rule struct {
	Match RuleMatch `yaml:"match" json:"match"`
	Set   RuleSet   `yaml:"set" json:"set"`
}

type compiledRule struct {
	match RuleMatch
	globs struct {
		source, repo, branch, event, workflow, cluster, objectKind *regexp.Regexp
		paths                                                      []*regexp.Regexp
	}
	set map[string]*template.Template // field name → value template
}

// Engine evaluates ordered rules: every rule is tried (no short-circuit); a
// matching rule fills only fields still unset (first-writer-wins per field).
type Engine struct {
	rules []compiledRule
}

var tmplFuncs = template.FuncMap{
	"trimPrefix": func(s, prefix string) string { return strings.TrimPrefix(s, prefix) },
	"trimSuffix": func(s, suffix string) string { return strings.TrimSuffix(s, suffix) },
	"lower":      strings.ToLower,
	"regexReplace": func(s, pattern, repl string) (string, error) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("regexReplace: %w", err)
		}
		return re.ReplaceAllString(s, repl), nil
	},
}

// NewEngine compiles globs and templates up front so config errors surface at
// startup, not per event.
func NewEngine(rules []Rule) (*Engine, error) {
	e := &Engine{}
	for i, r := range rules {
		var c compiledRule
		c.match = r.Match

		var err error
		compile := func(dst **regexp.Regexp, pattern string) {
			if err != nil || pattern == "" {
				return
			}
			*dst, err = compileGlob(pattern)
		}
		compile(&c.globs.source, r.Match.Source)
		compile(&c.globs.repo, r.Match.Repo)
		compile(&c.globs.branch, r.Match.Branch)
		compile(&c.globs.event, r.Match.Event)
		compile(&c.globs.workflow, r.Match.Workflow)
		compile(&c.globs.cluster, r.Match.Cluster)
		compile(&c.globs.objectKind, r.Match.ObjectKind)
		for _, p := range r.Match.Paths {
			if err != nil {
				break
			}
			var re *regexp.Regexp
			if re, err = compileGlob(p); err == nil {
				c.globs.paths = append(c.globs.paths, re)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("rules[%d]: %w", i, err)
		}

		c.set = map[string]*template.Template{}
		for field, value := range map[string]string{
			"env": r.Set.Env, "cluster": r.Set.Cluster, "namespace": r.Set.Namespace,
			"service": r.Set.Service, "kind": r.Set.Kind, "actor": r.Set.Actor,
		} {
			if value == "" {
				continue
			}
			tmpl, err := template.New(field).Funcs(tmplFuncs).Parse(value)
			if err != nil {
				return nil, fmt.Errorf("rules[%d].set.%s: %w", i, field, err)
			}
			c.set[field] = tmpl
		}
		e.rules = append(e.rules, c)
	}
	return e, nil
}

// Apply runs the rules over ev, filling unset fields, then redacts. Env stays
// "" when nothing matched — surfaced by doctor, never guessed (trap #2).
func (e *Engine) Apply(ev *model.Event, f Facts) error {
	for i := range e.rules {
		r := &e.rules[i]
		if !r.matches(f) {
			continue
		}
		for field, tmpl := range r.set {
			if !fieldEmpty(ev, field) {
				continue // first-writer-wins
			}
			var b strings.Builder
			if err := tmpl.Execute(&b, f); err != nil {
				return fmt.Errorf("rule %d set %s: %w", i, field, err)
			}
			setField(ev, field, b.String())
		}
	}
	RedactEvent(ev)
	return nil
}

func (r *compiledRule) matches(f Facts) bool {
	check := func(re *regexp.Regexp, val string) bool { return re == nil || re.MatchString(val) }
	if !check(r.globs.source, f.Source) || !check(r.globs.repo, f.Repo) ||
		!check(r.globs.branch, f.Branch) || !check(r.globs.event, f.Event) ||
		!check(r.globs.workflow, f.Workflow) ||
		!check(r.globs.cluster, f.Cluster) || !check(r.globs.objectKind, f.ObjectKind) {
		return false
	}
	if len(r.globs.paths) > 0 {
		if f.PathsTruncated {
			return false // unknown file list: skip, don't misroute (trap #3)
		}
		any := false
		for _, re := range r.globs.paths {
			for _, p := range f.Paths {
				if re.MatchString(p) {
					any = true
					break
				}
			}
			if any {
				break
			}
		}
		if !any {
			return false
		}
	}
	return true
}

func fieldEmpty(ev *model.Event, field string) bool {
	switch field {
	case "env":
		return ev.Env == ""
	case "cluster":
		return ev.Cluster == ""
	case "namespace":
		return ev.Namespace == ""
	case "service":
		return ev.Service == ""
	case "kind":
		return ev.Kind == ""
	case "actor":
		return ev.Actor == ""
	}
	return false
}

func setField(ev *model.Event, field, value string) {
	switch field {
	case "env":
		ev.Env = value
	case "cluster":
		ev.Cluster = value
	case "namespace":
		ev.Namespace = value
	case "service":
		ev.Service = value
	case "kind":
		if model.ValidKind(model.Kind(value)) {
			ev.Kind = model.Kind(value)
		}
	case "actor":
		ev.Actor = value
	}
}

// compileGlob turns a glob with * (one path segment) and ** (any depth) into
// an anchored regexp. Everything else is literal.
func compileGlob(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch {
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++ // consume second *
		case pattern[i] == '*':
			b.WriteString("[^/]*")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", pattern, err)
	}
	return re, nil
}
