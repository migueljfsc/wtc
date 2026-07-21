package query

import (
	"context"
	"sort"
	"time"

	"github.com/migueljfsc/wtc/internal/store"
)

// DiffRow compares one service between two environments.
type DiffRow struct {
	Service      string     `json:"service"`
	A            string     `json:"a"` // artifact (fallback ref) in env A; "" = not deployed
	B            string     `json:"b"` // same for env B
	ATS          *time.Time `json:"a_ts,omitempty"`
	BTS          *time.Time `json:"b_ts,omitempty"`
	DriftSeconds *int64     `json:"drift_seconds,omitempty"` // |aTS - bTS| when both exist and differ
	LastActor    string     `json:"last_actor,omitempty"`    // actor of the newer side
	InSync       bool       `json:"in_sync"`
	OnlyIn       string     `json:"only_in,omitempty"` // env name when deployed to exactly one side
	RevisionOnly bool       `json:"revision_only"`     // compared refs because artifact data is missing
}

// DiffReport is the full A-vs-B comparison.
type DiffReport struct {
	EnvA string    `json:"env_a"`
	EnvB string    `json:"env_b"`
	Rows []DiffRow `json:"rows"`
}

// Diff compares the latest successful deploy of every service present in
// either env (SPEC §6). Comparison key is artifact; when a side lacks
// artifact data the refs are compared and the row is flagged revision-only.
// A non-zero asOf reconstructs the comparison as of that instant (point-in-time
// state); the zero value compares current state.
func Diff(ctx context.Context, st *store.Store, envA, envB string, asOf time.Time) (*DiffReport, error) {
	latest, err := st.LatestSucceededDeploys(ctx, []string{envA, envB}, asOf)
	if err != nil {
		return nil, err
	}

	type pair struct{ a, b *int }
	services := map[string]pair{}
	for i := range latest {
		ev := latest[i]
		p := services[ev.Service]
		idx := i
		if ev.Env == envA {
			p.a = &idx
		} else {
			p.b = &idx
		}
		services[ev.Service] = p
	}

	r := &DiffReport{EnvA: envA, EnvB: envB, Rows: []DiffRow{}} // rows: [] on empty, never null
	for svc, p := range services {
		row := DiffRow{Service: svc}
		version := func(i *int) (string, bool) { // value + whether it fell back to ref
			if i == nil {
				return "", false
			}
			ev := latest[*i]
			if ev.Artifact != "" {
				return ev.Artifact, false
			}
			return short(ev.Ref), ev.Ref != ""
		}
		var aRevOnly, bRevOnly bool
		row.A, aRevOnly = version(p.a)
		row.B, bRevOnly = version(p.b)
		row.RevisionOnly = aRevOnly || bRevOnly

		if p.a != nil {
			ts := latest[*p.a].TS
			row.ATS = &ts
		}
		if p.b != nil {
			ts := latest[*p.b].TS
			row.BTS = &ts
		}

		switch {
		case p.a == nil:
			row.OnlyIn = envB
			row.LastActor = latest[*p.b].Actor
		case p.b == nil:
			row.OnlyIn = envA
			row.LastActor = latest[*p.a].Actor
		default:
			row.InSync = row.A == row.B
			drift := row.ATS.Sub(*row.BTS)
			if drift < 0 {
				drift = -drift
			}
			if !row.InSync {
				d := int64(drift.Seconds())
				row.DriftSeconds = &d
			}
			newer := latest[*p.a]
			if row.BTS.After(*row.ATS) {
				newer = latest[*p.b]
			}
			row.LastActor = newer.Actor
		}
		r.Rows = append(r.Rows, row)
	}

	sort.Slice(r.Rows, func(i, j int) bool { return r.Rows[i].Service < r.Rows[j].Service })
	return r, nil
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
