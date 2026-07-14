package query

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/store"
)

// HandoffReport is the digest for `wtc handoff` (SPEC §6). Alert correlation
// arrives with phase 5.
type HandoffReport struct {
	Since        time.Time           `json:"since"`
	DeploysByEnv map[string]EnvStats `json:"deploys_by_env"`
	Failures     []model.Event       `json:"failures,omitempty"` // failed deploys in window
	InfraChanges int                 `json:"infra_changes"`
	Rollbacks    int                 `json:"rollbacks"`
	Unmapped     int                 `json:"unmapped"`
	TopActors    []ActorCount        `json:"top_actors,omitempty"`
	NewServices  []string            `json:"new_services,omitempty"` // first ever seen inside the window
}

// EnvStats counts one env's deploys.
type EnvStats struct {
	Total  int `json:"total"`
	Failed int `json:"failed"`
}

// ActorCount is one leaderboard entry.
type ActorCount struct {
	Actor string `json:"actor"`
	Count int    `json:"count"`
}

// Handoff aggregates the window's activity.
func Handoff(ctx context.Context, st *store.Store, since time.Time) (*HandoffReport, error) {
	all, _, err := st.ListEvents(ctx, store.Filter{Since: since, Limit: 1000})
	if err != nil {
		return nil, err
	}

	r := &HandoffReport{Since: since.UTC(), DeploysByEnv: map[string]EnvStats{}}
	actorCounts := map[string]int{}
	for _, ev := range all {
		if ev.Actor != "" {
			actorCounts[ev.Actor]++
		}
		if ev.Env == "" {
			r.Unmapped++
		}
		switch ev.Kind {
		case model.KindDeploy:
			s := r.DeploysByEnv[ev.Env]
			s.Total++
			if ev.Status == model.StatusFailed {
				s.Failed++
				r.Failures = append(r.Failures, ev)
			}
			r.DeploysByEnv[ev.Env] = s
		case model.KindInfraChange:
			r.InfraChanges++
		case model.KindRollback:
			r.Rollbacks++
		}
	}

	for actor, n := range actorCounts {
		r.TopActors = append(r.TopActors, ActorCount{Actor: actor, Count: n})
	}
	sortActors(r.TopActors)
	if len(r.TopActors) > 5 {
		r.TopActors = r.TopActors[:5]
	}

	newSvcs, err := st.ServicesFirstSeenSince(ctx, since)
	if err != nil {
		return nil, err
	}
	r.NewServices = newSvcs
	return r, nil
}

func sortActors(a []ActorCount) {
	for i := 1; i < len(a); i++ { // tiny n: insertion sort keeps ties stable
		for j := i; j > 0 && (a[j].Count > a[j-1].Count || (a[j].Count == a[j-1].Count && a[j].Actor < a[j-1].Actor)); j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// Markdown renders the digest for terminals and future Slack posting.
func (r *HandoffReport) Markdown(now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Change handoff — since %s\n\n", r.Since.Local().Format("2006-01-02 15:04"))

	fmt.Fprintf(&b, "## Deploys\n\n")
	if len(r.DeploysByEnv) == 0 {
		b.WriteString("no deploys in window\n")
	}
	for env, s := range r.DeploysByEnv {
		name := env
		if name == "" {
			name = "(unmapped)"
		}
		fmt.Fprintf(&b, "- **%s**: %d deploys, %d failed\n", name, s.Total, s.Failed)
	}
	if len(r.Failures) > 0 {
		fmt.Fprintf(&b, "\n### Failures\n\n")
		for _, ev := range r.Failures {
			fmt.Fprintf(&b, "- [%s] %s (%s)\n", ev.Env, ev.Title, ev.TS.Local().Format("Jan 2 15:04"))
		}
	}

	fmt.Fprintf(&b, "\n## Other changes\n\n- infra changes: %d\n- rollbacks: %d\n- unmapped events: %d\n",
		r.InfraChanges, r.Rollbacks, r.Unmapped)

	if len(r.TopActors) > 0 {
		fmt.Fprintf(&b, "\n## Top actors\n\n")
		for _, a := range r.TopActors {
			fmt.Fprintf(&b, "- %s (%d)\n", a.Actor, a.Count)
		}
	}
	if len(r.NewServices) > 0 {
		fmt.Fprintf(&b, "\n## First-seen services\n\n")
		for _, s := range r.NewServices {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	return b.String()
}
