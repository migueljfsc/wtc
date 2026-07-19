package normalize

import (
	"fmt"
	"regexp"
)

// Ingest scope filtering for the push-only sources (Flux, ArgoCD): an
// allow/deny list applied at ingest so third-party reconciles/apps (cert-
// manager, external-dns, operator CRDs, …) never enter the ledger. The poller
// sources (GitHub, GitLab) scope instead via repos/projects; this is
// their analog for sources that have no poll list.
//
// Semantics:
//   - deny wins over allow — a broad allow with a specific deny is the shape;
//   - an empty allow list means "allow everything";
//   - an empty deny list means "deny nothing".
//
// Fields WITHIN one entry are AND; the list of entries is OR. Matching is on
// raw ingest facts only (namespace, object name, kind, cluster, project) —
// never inferred env/service — so the drop decision is deterministic and
// independent of the rules engine that runs after it. Globs use the shared
// CompileGlob dialect (`*` one segment, `**` any depth).

// ScopeMatch selects events by raw facts. Empty fields are unconstrained;
// each non-empty field is a glob. For ArgoCD, ObjectName is the app name and
// Cluster is unused (its destination is a server URL, never an env); for Flux,
// Project is unused.
type ScopeMatch struct {
	Namespace  string `yaml:"namespace" json:"namespace,omitempty"`
	ObjectName string `yaml:"object_name" json:"object_name,omitempty"` // Flux involvedObject.name / Argo app
	ObjectKind string `yaml:"object_kind" json:"object_kind,omitempty"` // e.g. Kustomization, HelmRelease, Application
	Cluster    string `yaml:"cluster" json:"cluster,omitempty"`         // Flux only
	Project    string `yaml:"project" json:"project,omitempty"`         // ArgoCD only
}

func (m ScopeMatch) isEmpty() bool {
	return m.Namespace == "" && m.ObjectName == "" && m.ObjectKind == "" &&
		m.Cluster == "" && m.Project == ""
}

// ScopeFilter is a source's allow/deny scope as configured. The zero value
// constrains nothing (all events pass). Compile it once at startup.
type ScopeFilter struct {
	Allow []ScopeMatch `yaml:"allow" json:"allow,omitempty"`
	Deny  []ScopeMatch `yaml:"deny" json:"deny,omitempty"`
}

// IsZero reports whether the filter constrains nothing — the common case.
func (f ScopeFilter) IsZero() bool { return len(f.Allow) == 0 && len(f.Deny) == 0 }

type compiledMatch struct {
	namespace, objectName, objectKind, cluster, project *regexp.Regexp
}

// CompiledScope is a ScopeFilter with every glob compiled. Build via
// ScopeFilter.Compile; Permit is then allocation-free. A nil *CompiledScope
// permits everything, so callers may hold nil for "no scope configured".
type CompiledScope struct {
	allow []compiledMatch
	deny  []compiledMatch
}

func compileMatch(m ScopeMatch) (compiledMatch, error) {
	var c compiledMatch
	var err error
	set := func(dst **regexp.Regexp, pattern string) {
		if err != nil || pattern == "" {
			return
		}
		*dst, err = CompileGlob(pattern)
	}
	set(&c.namespace, m.Namespace)
	set(&c.objectName, m.ObjectName)
	set(&c.objectKind, m.ObjectKind)
	set(&c.cluster, m.Cluster)
	set(&c.project, m.Project)
	return c, err
}

// Compile validates and compiles every glob up front so config errors surface
// at startup, not per event. An entry with no fields set is rejected: an empty
// allow entry would be a no-op and an empty deny entry would silently drop all
// ingest — always a config mistake.
func (f ScopeFilter) Compile() (*CompiledScope, error) {
	cs := &CompiledScope{}
	compileList := func(in []ScopeMatch, label string) ([]compiledMatch, error) {
		out := make([]compiledMatch, 0, len(in))
		for i, m := range in {
			if m.isEmpty() {
				return nil, fmt.Errorf("%s[%d]: entry sets no fields (matches everything) — remove it or add a constraint", label, i)
			}
			c, err := compileMatch(m)
			if err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", label, i, err)
			}
			out = append(out, c)
		}
		return out, nil
	}
	var err error
	if cs.allow, err = compileList(f.Allow, "allow"); err != nil {
		return nil, err
	}
	if cs.deny, err = compileList(f.Deny, "deny"); err != nil {
		return nil, err
	}
	return cs, nil
}

func (c compiledMatch) matches(f Facts) bool {
	// AND across set fields; an unset (nil) field is unconstrained. An empty
	// fact value only matches an unset field — a "prod-*" namespace glob never
	// matches an event with no namespace.
	if c.namespace != nil && !c.namespace.MatchString(f.Namespace) {
		return false
	}
	if c.objectName != nil && !c.objectName.MatchString(f.ObjectName) {
		return false
	}
	if c.objectKind != nil && !c.objectKind.MatchString(f.ObjectKind) {
		return false
	}
	if c.cluster != nil && !c.cluster.MatchString(f.Cluster) {
		return false
	}
	if c.project != nil && !c.project.MatchString(f.Project) {
		return false
	}
	return true
}

func anyMatch(ms []compiledMatch, f Facts) bool {
	for _, m := range ms {
		if m.matches(f) {
			return true
		}
	}
	return false
}

// Permit applies the scope decision: deny wins; an empty allow
// list permits all; otherwise the facts must match some allow entry. A nil
// receiver (no scope configured) permits everything.
func (cs *CompiledScope) Permit(f Facts) bool {
	if cs == nil {
		return true
	}
	if anyMatch(cs.deny, f) {
		return false
	}
	if len(cs.allow) == 0 {
		return true
	}
	return anyMatch(cs.allow, f)
}
