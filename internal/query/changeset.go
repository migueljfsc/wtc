package query

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

// Changeset collapses every event carrying one app commit sha into a single
// logical change: the build, the merge/push, and the per-env deploys. It is a
// `where` journey summarized for a list view.
type Changeset struct {
	Sha      string    `json:"sha"`      // longest known form of the app commit sha
	Title    string    `json:"title"`    // representative (merge > build > other)
	Services []string  `json:"services"` // distinct services touched
	Envs     []string  `json:"envs"`     // envs with a succeeded deploy of this sha
	Clusters []string  `json:"clusters"` // clusters with a succeeded deploy of this sha
	Owners   []string  `json:"owners"`   // distinct owning teams
	Repos    []string  `json:"repos"`    // distinct source repos
	Actors   []string  `json:"actors"`   // distinct actors
	Kinds    []string  `json:"kinds"`    // distinct kinds present
	Refs     []string  `json:"refs"`     // every ref in the change (app sha + manifests revisions); filters the timeline to it
	FirstTS  time.Time `json:"first_ts"` // earliest event
	LastTS   time.Time `json:"last_ts"`  // latest event
	Events   int       `json:"events"`   // events folded in
	Failed   bool      `json:"failed"`   // any failed/degraded event
	Deployed bool      `json:"deployed"` // any succeeded deploy
}

// ChangesetsReport lists the changesets active in a window, newest-first by
// their most recent event. Events with no resolvable app sha (flux reconciles
// with no artifact, alerts) are not part of any changeset.
type ChangesetsReport struct {
	Since      time.Time   `json:"since"`
	Until      time.Time   `json:"until"`
	Changesets []Changeset `json:"changesets"`
}

type csAccum struct {
	sha       string // longest form seen
	title     string
	titleRank int
	services  set
	envs      set
	clusters  set
	owners    set
	repos     set
	actors    set
	kinds     set
	refs      set
	firstTS   time.Time
	lastTS    time.Time
	events    int
	failed    bool
	deployed  bool
}

// titleRank prefers a merge's PR title, then a build's, over anything else —
// the intent describes the change best.
func titleRank(k model.Kind) int {
	switch k {
	case model.KindMerge:
		return 3
	case model.KindBuild:
		return 2
	case model.KindDeploy:
		return 1
	default:
		return 0
	}
}

func (a *csAccum) add(e model.Event, envReached bool) {
	if len(e.Ref) > len(a.sha) && refsMatch(e.Ref, a.sha) {
		a.sha = e.Ref // expand to the longest sha form
	}
	if r := titleRank(e.Kind); r > a.titleRank || (r == a.titleRank && a.title == "") {
		a.title, a.titleRank = e.Title, r
	}
	a.services.add(e.Service)
	a.owners.add(e.Owner)
	a.repos.add(e.Repo)
	a.actors.add(e.Actor)
	a.kinds.add(string(e.Kind))
	a.refs.add(e.Ref)
	if envReached {
		a.envs.add(e.Env)
		a.clusters.add(e.Cluster)
	}
	if a.firstTS.IsZero() || e.TS.Before(a.firstTS) {
		a.firstTS = e.TS
	}
	if e.TS.After(a.lastTS) {
		a.lastTS = e.TS
	}
	a.events++
	if e.Status == model.StatusFailed || e.Status == model.StatusDegraded {
		a.failed = true
	}
	if e.Kind == model.KindDeploy && e.Status == model.StatusSucceeded {
		a.deployed = true
	}
}

// Changesets groups the window's build/merge/push/deploy events by the app
// commit sha they carry, mirroring `where`: a deploy joins via its artifact tag
// or via a manifests revision an intent bumped.
func Changesets(ctx context.Context, st *store.Store, tags *normalize.TagResolver, since, until time.Time, scope store.AggScope) (*ChangesetsReport, error) {
	evs, err := st.EventsInWindow(ctx, since, until,
		[]model.Kind{model.KindBuild, model.KindMerge, model.KindPush, model.KindDeploy})
	if err != nil {
		return nil, err
	}

	groups := map[string]*csAccum{}
	get := func(key, full string) *csAccum {
		g := groups[key]
		if g == nil {
			g = &csAccum{sha: full, services: set{}, envs: set{}, clusters: set{}, owners: set{}, repos: set{}, actors: set{}, kinds: set{}, refs: set{}}
			groups[key] = g
		}
		return g
	}
	revToSha := map[string]string{} // manifests revision → short app sha

	// Pass 1: intents and builds anchor the shas. A merge/push whose sha came
	// from a payload bump (not its own ref) maps its ref (a manifests revision)
	// to the sha, so a deploy of that revision joins the same changeset.
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
		get(key, sha).add(e, false)
		if e.Ref != "" && !refsMatch(e.Ref, sha) {
			revToSha[e.Ref] = key
		}
	}

	// Pass 2: attach deploys via artifact-tag resolution, a matching manifests
	// revision, or a ref that is itself the app sha (e.g. dev image-automation).
	for _, d := range deploys {
		key, full := "", ""
		if d.Artifact != "" {
			if s := tags.Resolve(d.Artifact); s != "" {
				key, full = short(s), s
			}
		}
		if key == "" && d.Ref != "" {
			if s, ok := revToSha[d.Ref]; ok {
				key, full = s, s
			}
		}
		if key == "" && hexRef.MatchString(d.Ref) {
			if _, ok := groups[short(d.Ref)]; ok {
				key, full = short(d.Ref), d.Ref
			}
		}
		if key == "" {
			continue
		}
		get(key, full).add(d, d.Status == model.StatusSucceeded)
	}

	out := make([]Changeset, 0, len(groups))
	for _, g := range groups {
		out = append(out, Changeset{
			Sha:      g.sha,
			Title:    g.title,
			Services: g.services.sorted(),
			Envs:     g.envs.sorted(),
			Clusters: g.clusters.sorted(),
			Owners:   g.owners.sorted(),
			Repos:    g.repos.sorted(),
			Actors:   g.actors.sorted(),
			Kinds:    g.kinds.sorted(),
			Refs:     g.refs.sorted(),
			FirstTS:  g.firstTS,
			LastTS:   g.lastTS,
			Events:   g.events,
			Failed:   g.failed,
			Deployed: g.deployed,
		})
	}

	// Scope filter, changeset-level: keep changes that reached a scoped env or
	// cluster, touched a scoped service, and are owned by a scoped team.
	// (Event-level filtering would wrongly drop a change's build/merge, which
	// carry no env/cluster.)
	if len(scope.Envs) > 0 || len(scope.Clusters) > 0 || len(scope.Services) > 0 || len(scope.Owners) > 0 {
		kept := make([]Changeset, 0, len(out))
		for _, cs := range out {
			if anyIn(scope.Envs, cs.Envs) && anyIn(scope.Clusters, cs.Clusters) &&
				anyIn(scope.Services, cs.Services) && anyIn(scope.Owners, cs.Owners) {
				kept = append(kept, cs)
			}
		}
		out = kept
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastTS.After(out[j].LastTS) })

	return &ChangesetsReport{Since: since.UTC(), Until: until.UTC(), Changesets: out}, nil
}

// intentSha resolves the app sha a build/merge/push carries: a build's or push's
// hex ref, or a merge's payload image-bump tag.
func intentSha(e model.Event, tags *normalize.TagResolver) string {
	if e.Artifact != "" {
		if s := tags.Resolve(e.Artifact); s != "" {
			return s
		}
	}
	if (e.Kind == model.KindBuild || e.Kind == model.KindPush) && hexRef.MatchString(e.Ref) {
		return e.Ref
	}
	if e.Kind == model.KindMerge || e.Kind == model.KindPush {
		if s := firstBumpSha(e.Payload, tags); s != "" {
			return s
		}
	}
	return ""
}

// firstBumpSha returns the sha the first resolvable image bump in payload maps
// to, "" when none.
func firstBumpSha(payload string, tags *normalize.TagResolver) string {
	if payload == "" {
		return ""
	}
	var p struct {
		ImageBumps []struct {
			New string `json:"new"`
		} `json:"image_bumps"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return ""
	}
	for _, b := range p.ImageBumps {
		if s := tags.Resolve(b.New); s != "" {
			return s
		}
	}
	return ""
}

// anyIn reports whether any wanted value appears in have. An empty want is
// unconstrained (matches anything).
func anyIn(want, have []string) bool {
	if len(want) == 0 {
		return true
	}
	set := make(map[string]bool, len(have))
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if set[w] {
			return true
		}
	}
	return false
}

// set is a small string set with sorted, [] (never nil) output.
type set map[string]bool

func (s set) add(v string) {
	if v != "" {
		s[v] = true
	}
}

func (s set) sorted() []string {
	out := make([]string, 0, len(s))
	for v := range s {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
