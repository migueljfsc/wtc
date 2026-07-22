package query

import (
	"context"
	"sort"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

// DefaultDORAWindow is how long after a deploy an alert or rollback is still
// attributed to it for the change-failure rate.
const DefaultDORAWindow = time.Hour

// DORAMetrics is the change-failure rate and MTTR for a scope (overall, an env,
// or an owner). Change-failure rate is a fraction 0..1; MTTR is nil when no
// incident resolved in the window.
type DORAMetrics struct {
	Deploys           int      `json:"deploys"`                // terminal deploy events (succeeded + failed)
	Failures          int      `json:"failures"`               // deploys that failed or were followed by an alert/rollback
	ChangeFailureRate float64  `json:"change_failure_rate"`    // failures / deploys; 0 when no deploys
	Incidents         int      `json:"incidents"`              // resolved alerts counted for MTTR
	MTTRSeconds       *float64 `json:"mttr_seconds,omitempty"` // mean alert firing→resolved; nil when none
}

// DORAGroup is one scope's metrics, keyed by env or owner. The metrics flatten
// into the object (anonymous embedding), so a group is {key, deploys, …}.
type DORAGroup struct {
	Key string `json:"key"`
	DORAMetrics
}

// DORAReport is deploy-quality metrics over a window: overall, per env, and per
// owning team. Deploy frequency lives in the separate deploy-stats endpoint;
// lead time is per env (build/merge → deploy).
type DORAReport struct {
	Since         time.Time       `json:"since"`
	Until         time.Time       `json:"until"`
	WindowSeconds int             `json:"window_seconds"` // failure-attribution window after a deploy
	Overall       DORAMetrics     `json:"overall"`
	ByEnv         []DORAGroup     `json:"by_env"`
	ByOwner       []DORAGroup     `json:"by_owner"`
	LeadTime      []LeadTimeGroup `json:"lead_time"` // build/merge → deploy, per env: median + p90
}

type doraAccum struct {
	deploys, failures, incidents int
	mttrSum                      float64 // seconds
}

func (a *doraAccum) metrics() DORAMetrics {
	m := DORAMetrics{Deploys: a.deploys, Failures: a.failures, Incidents: a.incidents}
	if a.deploys > 0 {
		m.ChangeFailureRate = float64(a.failures) / float64(a.deploys)
	}
	if a.incidents > 0 {
		v := a.mttrSum / float64(a.incidents)
		m.MTTRSeconds = &v
	}
	return m
}

// DORA computes change-failure rate and MTTR over [since, until]. A deployment
// (terminal deploy) counts as a failure if it failed outright, or an alert or
// rollback followed it in the same env within window. MTTR averages the
// firing→resolved duration of resolved alerts. window <= 0 uses the default.
func DORA(ctx context.Context, st *store.Store, tags *normalize.TagResolver, since, until time.Time, window time.Duration, scope store.AggScope) (*DORAReport, error) {
	if window <= 0 {
		window = DefaultDORAWindow
	}
	evs, err := st.EventsInWindow(ctx, since, until,
		[]model.Kind{model.KindDeploy, model.KindAlert, model.KindRollback})
	if err != nil {
		return nil, err
	}

	var deploys, alerts []model.Event
	type signal struct {
		ts  time.Time
		env string
	}
	var signals []signal // alerts + rollbacks, for failure attribution
	for _, e := range evs {
		if !scope.Match(e.Env, e.Service, e.Owner) {
			continue
		}
		switch e.Kind {
		case model.KindDeploy:
			if e.Status == model.StatusSucceeded || e.Status == model.StatusFailed {
				deploys = append(deploys, e)
			}
		case model.KindAlert:
			alerts = append(alerts, e)
			signals = append(signals, signal{e.TS, e.Env})
		case model.KindRollback:
			signals = append(signals, signal{e.TS, e.Env})
		}
	}

	// A deploy is a failure if it failed, or a same-env alert/rollback lands in
	// (deploy, deploy+window]. Env is the correlation key — alerts often lack a
	// clean service, so matching on env keeps the signal robust.
	isFailure := func(d model.Event) bool {
		if d.Status == model.StatusFailed {
			return true
		}
		if d.Env == "" {
			return false
		}
		end := d.TS.Add(window)
		for _, s := range signals {
			if s.env == d.Env && s.ts.After(d.TS) && !s.ts.After(end) {
				return true
			}
		}
		return false
	}

	overall := &doraAccum{}
	byEnv := map[string]*doraAccum{}
	byOwner := map[string]*doraAccum{}
	// get returns the accumulator for a non-empty key, creating it on demand;
	// nil for "" so unmapped env/owner rows only feed the overall total.
	get := func(m map[string]*doraAccum, k string) *doraAccum {
		if k == "" {
			return nil
		}
		a := m[k]
		if a == nil {
			a = &doraAccum{}
			m[k] = a
		}
		return a
	}

	for _, d := range deploys {
		fail := isFailure(d)
		overall.deploys++
		if fail {
			overall.failures++
		}
		for _, a := range []*doraAccum{get(byEnv, d.Env), get(byOwner, d.Owner)} {
			if a != nil {
				a.deploys++
				if fail {
					a.failures++
				}
			}
		}
	}
	for _, al := range alerts {
		if al.DurationMS == nil { // still firing — no resolution time yet
			continue
		}
		secs := float64(*al.DurationMS) / 1000
		overall.incidents++
		overall.mttrSum += secs
		for _, a := range []*doraAccum{get(byEnv, al.Env), get(byOwner, al.Owner)} {
			if a != nil {
				a.incidents++
				a.mttrSum += secs
			}
		}
	}

	lead, err := leadTime(ctx, st, tags, since, until, scope)
	if err != nil {
		return nil, err
	}

	return &DORAReport{
		Since:         since.UTC(),
		Until:         until.UTC(),
		WindowSeconds: int(window.Seconds()),
		Overall:       overall.metrics(),
		ByEnv:         doraGroups(byEnv),
		ByOwner:       doraGroups(byOwner),
		LeadTime:      lead,
	}, nil
}

func doraGroups(m map[string]*doraAccum) []DORAGroup {
	out := make([]DORAGroup, 0, len(m))
	for k, a := range m {
		out = append(out, DORAGroup{Key: k, DORAMetrics: a.metrics()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
