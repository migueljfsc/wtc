package normalize

import (
	"sync/atomic"

	"github.com/migueljfsc/wtc/internal/model"
)

// EngineHolder wraps an Engine behind an atomic pointer so the rules can be
// hot-reloaded (P10) without restarting. Apply always runs the current engine;
// Swap installs a rebuilt one. Safe for concurrent Apply + Swap — every ingest
// path (webhook handlers + poller) holds the same holder, so an edit re-routes
// all of them at once.
type EngineHolder struct {
	p atomic.Pointer[Engine]
}

func NewEngineHolder(e *Engine) *EngineHolder {
	h := &EngineHolder{}
	h.p.Store(e)
	return h
}

// Apply runs the current engine's rules against ev given facts.
func (h *EngineHolder) Apply(ev *model.Event, f Facts) error {
	return h.p.Load().Apply(ev, f)
}

// Swap installs a newly-built engine as the current one.
func (h *EngineHolder) Swap(e *Engine) { h.p.Store(e) }

// TagResolverHolder is the tag→sha resolver equivalent of EngineHolder.
type TagResolverHolder struct {
	p atomic.Pointer[TagResolver]
}

func NewTagResolverHolder(t *TagResolver) *TagResolverHolder {
	h := &TagResolverHolder{}
	h.p.Store(t)
	return h
}

// Load returns the current resolver (passed to query.Where per request).
func (h *TagResolverHolder) Load() *TagResolver { return h.p.Load() }

// Swap installs a newly-built resolver.
func (h *TagResolverHolder) Swap(t *TagResolver) { h.p.Store(t) }
