package store

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// TestLoadSanity is the load check: with 10k events on disk, the queries
// behind `wtc log` and `wtc diff` must stay well under the 100ms SPEC budget.
// We take the median of several runs so one GC pause can't flake the gate.
func TestLoadSanity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load sanity in -short mode")
	}
	s := openTestStore(t)
	ctx := context.Background()

	const total = 10_000
	envs := []string{"dev", "staging", "prod"}
	svcs := []string{"api", "web", "worker", "billing", "gateway"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := range total {
		env := envs[i%len(envs)]
		svc := svcs[i%len(svcs)]
		ev := testEvent(fmt.Sprintf("load:%d", i), base.Add(time.Duration(i)*time.Minute), func(e *model.Event) {
			e.Env = env
			e.Service = svc
			e.Kind = model.KindDeploy
			e.Status = model.StatusSucceeded
			e.Artifact = fmt.Sprintf("ghcr.io/acme/%s:sha-%06x", svc, i)
		})
		if _, _, err := s.Ingest(ctx, ev); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	const budget = 100 * time.Millisecond
	cases := []struct {
		name string
		run  func() error
	}{
		{"log-page", func() error {
			_, _, err := s.ListEvents(ctx, Filter{Limit: 50})
			return err
		}},
		{"log-env-filter", func() error {
			_, _, err := s.ListEvents(ctx, Filter{Envs: []string{"prod"}, Limit: 50})
			return err
		}},
		{"diff-latest-deploys", func() error {
			_, err := s.LatestSucceededDeploys(ctx, []string{"staging", "prod"}, time.Time{}, AggScope{})
			return err
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			const runs = 5
			samples := make([]time.Duration, runs)
			for i := range runs {
				start := time.Now()
				if err := c.run(); err != nil {
					t.Fatalf("%s: %v", c.name, err)
				}
				samples[i] = time.Since(start)
			}
			sort.Slice(samples, func(a, b int) bool { return samples[a] < samples[b] })
			median := samples[runs/2]
			t.Logf("%s median=%s (budget %s) over %d events", c.name, median.Round(time.Microsecond), budget, total)
			if median > budget {
				t.Fatalf("%s median %s exceeds %s budget at %d events", c.name, median, budget, total)
			}
		})
	}
}
