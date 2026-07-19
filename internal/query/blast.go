package query

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/store"
)

// Blast (PLAN P20) ranks the changes most likely to have caused an alert —
// or, anchored on a change, the alerts that fired after it. The score is a
// documented deterministic heuristic (recency/env/service/kind/status),
// never ML: same inputs, same ranking.

// Blast directions. Causes looks BACK from the anchor at changes; effects
// looks FORWARD from a change at alerts ("did my deploy cause noise?").
const (
	DirectionCauses  = "causes"
	DirectionEffects = "effects"
)

// Fixed v1 scoring weights, documented in the CLI help. Tunability can come
// later; determinism is the contract.
const (
	blastRecencyMax  = 30 // linear within the window: at the anchor 30 → edge 0
	blastSameEnv     = 30 // the hard signal — same blast radius
	blastSameService = 20 // booster only: alerts often lack a clean service
	blastFailedBump  = 10 // a failed/degraded change right before an alert
)

// blastKindWeight biases toward kinds that alter running systems over kinds
// that merely record intent. Alerts are never suspects (weight absent — they
// are excluded from the candidate query entirely).
var blastKindWeight = map[model.Kind]int{
	model.KindDeploy:       15,
	model.KindRollback:     15,
	model.KindConfigChange: 15,
	model.KindInfraChange:  12,
	model.KindManual:       10,
	model.KindMerge:        5,
	model.KindPush:         5,
	model.KindBuild:        2,
}

const (
	blastDefaultWindow = 2 * time.Hour
	blastDefaultLimit  = 20
	blastMaxLimit      = 100
	blastCandidateCap  = 1000 // store maxLimit — one page of candidates
)

// BlastInput anchors a blast query. Anchor is the resolved anchor event (nil
// when anchored on a bare timestamp); TS is the anchor instant either way.
// Env/Service override the anchor's scoring context — required for a bare-ts
// anchor to enable the same-env signal.
type BlastInput struct {
	Anchor  *model.Event
	TS      time.Time
	Env     string
	Service string
	Window  time.Duration // <= 0 → 2h
	Limit   int           // <= 0 → 20, capped at 100
}

// BlastSuspect is one ranked candidate with its score breakdown.
type BlastSuspect struct {
	Event   model.Event `json:"event"`
	Score   int         `json:"score"`
	Reasons []string    `json:"reasons"`
}

// BlastReport is the ranked answer to "what likely caused this?" (causes) or
// "what fired after this?" (effects).
type BlastReport struct {
	Anchor    *model.Event   `json:"anchor,omitempty"` // nil for bare-ts anchors
	AnchorTS  time.Time      `json:"anchor_ts"`
	Direction string         `json:"direction"` // causes | effects
	Env       string         `json:"env,omitempty"`
	Service   string         `json:"service,omitempty"`
	WindowMS  int64          `json:"window_ms"`
	Suspects  []BlastSuspect `json:"suspects"`
	Notes     []string       `json:"notes,omitempty"`
}

// Blast ranks suspects around the anchor. Direction is inferred: an alert
// anchor (or a bare timestamp) looks back at changes; a change anchor looks
// forward at alerts. Env stays a ranking signal, never a hard filter — a
// mis-inferred alert env must not hide the true cause (trap #2).
func Blast(ctx context.Context, st *store.Store, in BlastInput) (*BlastReport, error) {
	window := in.Window
	if window <= 0 {
		window = blastDefaultWindow
	}
	limit := in.Limit
	if limit <= 0 {
		limit = blastDefaultLimit
	}
	if limit > blastMaxLimit {
		limit = blastMaxLimit
	}

	r := &BlastReport{
		Anchor:    in.Anchor,
		AnchorTS:  in.TS,
		Direction: DirectionCauses,
		WindowMS:  window.Milliseconds(),
		// Suspects starts non-nil: the JSON contract says array, never null.
		Suspects: []BlastSuspect{},
	}

	env, service := in.Env, in.Service
	if in.Anchor != nil {
		if env == "" {
			env = in.Anchor.Env
		}
		if service == "" {
			service = in.Anchor.Service
		}
		if in.Anchor.Kind != model.KindAlert {
			r.Direction = DirectionEffects
		}
	}
	r.Env, r.Service = env, service
	if env == "" {
		r.Notes = append(r.Notes,
			"anchor has no env — the same-env signal is disabled (pass --env to enable it)")
	}

	f := store.Filter{Limit: blastCandidateCap}
	if r.Direction == DirectionCauses {
		f.Since = in.TS.Add(-window)
		f.Until = in.TS
		for k := range blastKindWeight {
			f.Kinds = append(f.Kinds, string(k))
		}
		sort.Strings(f.Kinds) // deterministic SQL, map order is not
	} else {
		f.Since = in.TS
		f.Until = in.TS.Add(window)
		f.Kinds = []string{string(model.KindAlert)}
	}

	events, next, err := st.ListEvents(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("blast candidates: %w", err)
	}
	if next != "" {
		r.Notes = append(r.Notes, fmt.Sprintf(
			"window holds more than %d candidates — scored one page; narrow --window", blastCandidateCap))
	}

	for _, ev := range events {
		if in.Anchor != nil && ev.ID == in.Anchor.ID {
			continue // the anchor is never its own suspect
		}
		s := BlastSuspect{Event: ev}

		var age time.Duration
		var side string
		if r.Direction == DirectionCauses {
			age, side = in.TS.Sub(ev.TS), "before"
		} else {
			age, side = ev.TS.Sub(in.TS), "after"
		}
		rec := int(math.Round(blastRecencyMax * (1 - float64(age)/float64(window))))
		if rec < 0 {
			rec = 0
		}
		s.Score += rec
		s.Reasons = append(s.Reasons, fmt.Sprintf("%s %s", age.Round(time.Second), side))

		if env != "" && ev.Env == env {
			s.Score += blastSameEnv
			s.Reasons = append(s.Reasons, "same env ("+env+")")
		}
		if service != "" && ev.Service == service {
			s.Score += blastSameService
			s.Reasons = append(s.Reasons, "same service ("+service+")")
		}
		if r.Direction == DirectionCauses {
			if w := blastKindWeight[ev.Kind]; w > 0 {
				s.Score += w
				s.Reasons = append(s.Reasons, string(ev.Kind))
			}
			if ev.Status == model.StatusFailed || ev.Status == model.StatusDegraded {
				s.Score += blastFailedBump
				s.Reasons = append(s.Reasons, string(ev.Status))
			}
		}
		r.Suspects = append(r.Suspects, s)
	}

	anchorTS := in.TS
	sort.Slice(r.Suspects, func(i, j int) bool {
		a, b := r.Suspects[i], r.Suspects[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		da, db := absDur(a.Event.TS.Sub(anchorTS)), absDur(b.Event.TS.Sub(anchorTS))
		if da != db {
			return da < db
		}
		return a.Event.ID < b.Event.ID
	})
	if len(r.Suspects) > limit {
		r.Suspects = r.Suspects[:limit]
	}
	if len(r.Suspects) == 0 {
		noun := "changes"
		if r.Direction == DirectionEffects {
			noun = "alerts"
		}
		r.Notes = append(r.Notes, "no "+noun+" in the window")
	}
	return r, nil
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
