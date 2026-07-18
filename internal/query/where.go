// Package query implements the composed queries — where, diff, handoff —
// on top of the store's read pool. SPEC §6.
package query

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/store"
)

var hexRef = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// WhereEnv is one environment's view of a change's journey.
type WhereEnv struct {
	Env     string       `json:"env"`
	Intent  *model.Event `json:"intent,omitempty"`  // merge/push that targeted this env
	Applied *model.Event `json:"applied,omitempty"` // latest deploy carrying the change
	LagMS   *int64       `json:"lag_ms,omitempty"`  // applied.ts - intent.ts
	Unknown []string     `json:"unknown,omitempty"` // explicit gaps, never guessed
}

// WhereReport is the full BUILD → INTENT → APPLIED picture for one ref.
type WhereReport struct {
	Input   string        `json:"input"`
	Sha     string        `json:"sha"` // longest known form
	Builds  []model.Event `json:"builds"`
	Intents []model.Event `json:"intents"`
	Envs    []WhereEnv    `json:"envs"`
	Notes   []string      `json:"notes,omitempty"`
}

// Where composes a change's journey. input is a git sha (short or full) or
// an image tag resolvable through tag_patterns.
func Where(ctx context.Context, st *store.Store, tags *normalize.TagResolver, input string) (*WhereReport, error) {
	// Envs starts non-nil: it is built by conditional appends, and the JSON
	// contract says array, never null (a sha legitimately reaches no env —
	// e.g. superseded before any reconcile — and clients index the list).
	r := &WhereReport{Input: input, Envs: []WhereEnv{}}

	sha := strings.ToLower(strings.TrimSpace(input))
	if !hexRef.MatchString(sha) {
		resolved := tags.Resolve(input)
		if resolved == "" {
			return nil, fmt.Errorf("%q is neither a git sha nor a tag matching any configured tag_pattern", input)
		}
		r.Notes = append(r.Notes, fmt.Sprintf("tag %q resolved to sha %s", input, resolved))
		sha = resolved
	}
	r.Sha = sha

	// BUILD — kind=build with a ref prefix match.
	builds, err := st.EventsByRefPrefix(ctx, sha, []model.Kind{model.KindBuild})
	if err != nil {
		return nil, err
	}
	r.Builds = builds
	for _, b := range builds {
		if len(b.Ref) > len(r.Sha) && strings.HasPrefix(b.Ref, sha) {
			r.Sha = b.Ref // expand to the full sha when a build knows it
		}
	}
	if len(builds) == 0 {
		r.Notes = append(r.Notes, "no build events for this sha (unknown, not fatal)")
	}

	// INTENT — the commit itself merged/pushed, plus merges whose enriched
	// payload bumps a tag that resolves back to this sha.
	direct, err := st.EventsByRefPrefix(ctx, sha, []model.Kind{model.KindMerge, model.KindPush})
	if err != nil {
		return nil, err
	}
	needle := sha
	if len(needle) > 7 {
		needle = needle[:7] // sha-embedded tags always contain the short form
	}
	candidates, err := st.EventsPayloadContaining(ctx, needle, []model.Kind{model.KindMerge, model.KindPush})
	if err != nil {
		return nil, err
	}
	intents := map[string]model.Event{}
	for _, ev := range direct {
		intents[ev.ID] = ev
	}
	for _, ev := range candidates {
		if bumpsResolveTo(ev.Payload, tags, r.Sha) {
			intents[ev.ID] = ev
		}
	}
	r.Intents = sortByTS(intents)

	// APPLIED — deploys whose ref equals a candidate manifest revision, or
	// whose artifact carries a tag resolving to the sha.
	revisions := map[string]bool{r.Sha: true}
	for _, in := range r.Intents {
		if in.Ref != "" {
			revisions[in.Ref] = true
		}
	}
	deploysByID := map[string]model.Event{}
	for rev := range revisions {
		ds, err := st.EventsByRefPrefix(ctx, rev, []model.Kind{model.KindDeploy})
		if err != nil {
			return nil, err
		}
		for _, d := range ds {
			deploysByID[d.ID] = d
		}
	}
	artifactHits, err := st.EventsArtifactContaining(ctx, needle, []model.Kind{model.KindDeploy})
	if err != nil {
		return nil, err
	}
	for _, d := range artifactHits {
		if resolvesTo(tags, d.Artifact, r.Sha) {
			deploysByID[d.ID] = d
		}
	}

	// Group per env: latest deploy wins; pair with the intent whose ref
	// matches the deploy's revision (fallback: latest intent).
	byEnv := map[string][]model.Event{}
	for _, d := range deploysByID {
		byEnv[d.Env] = append(byEnv[d.Env], d)
	}
	for env, ds := range byEnv {
		sort.Slice(ds, func(i, j int) bool { return ds[i].TS.Before(ds[j].TS) })
		applied := ds[len(ds)-1]
		we := WhereEnv{Env: env, Applied: &applied}
		if env == "" {
			we.Unknown = append(we.Unknown, "deploy has no env — check rules / wtc doctor")
		}

		var intent *model.Event
		for i := range r.Intents {
			in := r.Intents[i]
			if refsMatch(in.Ref, applied.Ref) {
				intent = &in // later intents win (list is ts-ascending)
			}
		}
		if intent == nil && len(r.Intents) > 0 {
			intent = &r.Intents[len(r.Intents)-1]
			we.Unknown = append(we.Unknown, "intent matched by recency, not revision")
		}
		if intent != nil {
			we.Intent = intent
			lag := applied.TS.Sub(intent.TS).Milliseconds()
			we.LagMS = &lag
		} else {
			we.Unknown = append(we.Unknown, "no intent event found (merge/push not ingested or not enriched)")
		}
		r.Envs = append(r.Envs, we)
	}
	sort.Slice(r.Envs, func(i, j int) bool { return r.Envs[i].Env < r.Envs[j].Env })

	if len(r.Envs) == 0 {
		r.Notes = append(r.Notes, "not applied to any environment yet")
	}
	return r, nil
}

// bumpsResolveTo reports whether payload's image_bumps contain a new tag
// resolving to sha (prefix match — tags may embed the short form).
func bumpsResolveTo(payload string, tags *normalize.TagResolver, sha string) bool {
	if payload == "" {
		return false
	}
	var p struct {
		ImageBumps []struct {
			New string `json:"new"`
		} `json:"image_bumps"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return false
	}
	for _, b := range p.ImageBumps {
		if resolvesTo(tags, b.New, sha) {
			return true
		}
	}
	return false
}

func resolvesTo(tags *normalize.TagResolver, tag, sha string) bool {
	got := tags.Resolve(tag)
	if got == "" {
		return false
	}
	return refsMatch(got, sha)
}

// refsMatch reports whether two non-empty shas denote the same commit —
// equal, or one is a prefix of the other (short vs full form).
func refsMatch(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func sortByTS(m map[string]model.Event) []model.Event {
	out := make([]model.Event, 0, len(m))
	for _, ev := range m {
		out = append(out, ev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	return out
}
