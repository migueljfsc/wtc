package query

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

// LeadTimeGroup is the lead time to one environment: how long changes took to
// reach it, from a change first entering the pipeline (its earliest
// build/merge/push) to its first succeeded deploy there. Median and p90 are in
// seconds; both absent when no change reached the env in the window.
//
// This is pipeline lead time (build/merge → deploy) — the DORA "lead time for
// changes" derived from the artifacts wtc already stores, via the same tag↔sha
// join `where` performs. It is not literal first-commit→deploy: the commit's
// authored time arrives only with compare-API enrichment. It is windowed like
// the rest of DORA, so it is most accurate over a window at least as long as
// the promotion cycle (a build outside the window can't anchor the start).
type LeadTimeGroup struct {
	Env           string   `json:"env"`
	Samples       int      `json:"samples"`
	MedianSeconds *float64 `json:"median_seconds,omitempty"`
	P90Seconds    *float64 `json:"p90_seconds,omitempty"`
}

type ltAccum struct {
	startTS  time.Time            // earliest intent (build/merge/push) ts; zero = none seen
	deployTS map[string]time.Time // env → earliest succeeded deploy ts
	services set
	owners   set
}

// leadTime groups the window's build/merge/push/deploy events by the app commit
// sha they carry (mirroring Changesets) and derives, per env, the median and
// p90 of the intent→deploy durations.
func leadTime(ctx context.Context, st *store.Store, tags *normalize.TagResolver, since, until time.Time, scope store.AggScope) ([]LeadTimeGroup, error) {
	evs, err := st.EventsInWindow(ctx, since, until,
		[]model.Kind{model.KindBuild, model.KindMerge, model.KindPush, model.KindDeploy})
	if err != nil {
		return nil, err
	}

	groups := map[string]*ltAccum{}
	get := func(key string) *ltAccum {
		g := groups[key]
		if g == nil {
			g = &ltAccum{deployTS: map[string]time.Time{}, services: set{}, owners: set{}}
			groups[key] = g
		}
		return g
	}
	revToSha := map[string]string{} // manifests revision → short app sha

	// Pass 1: intents (build/merge/push) anchor each sha and its start time.
	var deploys []model.Event
	for _, e := range evs {
		if e.Kind == model.KindDeploy {
			deploys = append(deploys, e)
			continue
		}
		sha := intentSha(e, tags)
		if sha == "" {
			continue
		}
		key := short(sha)
		g := get(key)
		if g.startTS.IsZero() || e.TS.Before(g.startTS) {
			g.startTS = e.TS
		}
		g.services.add(e.Service)
		g.owners.add(e.Owner)
		if e.Ref != "" && !refsMatch(e.Ref, sha) {
			revToSha[e.Ref] = key
		}
	}

	// Pass 2: attach succeeded deploys via artifact tag, a bumped manifests
	// revision, or a ref that is itself the app sha — same joins as Changesets.
	for _, d := range deploys {
		if d.Status != model.StatusSucceeded {
			continue
		}
		key := ""
		if d.Artifact != "" {
			if s := tags.Resolve(d.Artifact); s != "" {
				key = short(s)
			}
		}
		if key == "" && d.Ref != "" {
			if s, ok := revToSha[d.Ref]; ok {
				key = s
			}
		}
		if key == "" && hexRef.MatchString(d.Ref) {
			if _, ok := groups[short(d.Ref)]; ok {
				key = short(d.Ref)
			}
		}
		g := groups[key]
		if key == "" || g == nil { // sha never entered the pipeline in-window: can't time it
			continue
		}
		g.services.add(d.Service)
		g.owners.add(d.Owner)
		if cur, ok := g.deployTS[d.Env]; !ok || d.TS.Before(cur) {
			g.deployTS[d.Env] = d.TS
		}
	}

	// Per-env samples = deploy − start, with a change-level scope filter (a
	// change counts if it touched a scoped service and is owned by a scoped
	// team); env is filtered per sample.
	samples := map[string][]float64{}
	for _, g := range groups {
		if g.startTS.IsZero() {
			continue
		}
		if !anyIn(scope.Services, g.services.sorted()) || !anyIn(scope.Owners, g.owners.sorted()) {
			continue
		}
		for env, dts := range g.deployTS {
			if env == "" || (len(scope.Envs) > 0 && !anyIn(scope.Envs, []string{env})) {
				continue
			}
			if dts.Before(g.startTS) { // clock skew / out-of-order: no negative lead time
				continue
			}
			samples[env] = append(samples[env], dts.Sub(g.startTS).Seconds())
		}
	}

	out := make([]LeadTimeGroup, 0, len(samples))
	for env, xs := range samples {
		sort.Float64s(xs)
		med, p90 := percentile(xs, 0.5), percentile(xs, 0.9)
		out = append(out, LeadTimeGroup{Env: env, Samples: len(xs), MedianSeconds: &med, P90Seconds: &p90})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Env < out[j].Env })
	return out, nil
}

// percentile returns the p-quantile (p in [0,1]) of an ascending-sorted slice,
// linearly interpolating between adjacent ranks. Empty → 0.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	rank := p * float64(n-1)
	lo, hi := int(math.Floor(rank)), int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	frac := rank - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}
