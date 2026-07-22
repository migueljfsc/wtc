package query

import (
	"context"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/store"
)

func leadGroup(groups []LeadTimeGroup, env string) *LeadTimeGroup {
	for i := range groups {
		if groups[i].Env == env {
			return &groups[i]
		}
	}
	return nil
}

// The seed's demo-api change (built at t0) reaches dev at +3m, staging at +12m,
// and prod at +2h5m. Lead time to each env is that offset from the build.
func TestLeadTime(t *testing.T) {
	st := seed(t)
	lt, err := leadTime(context.Background(), st, resolver(t), t0.Add(-time.Hour), t0.Add(4*time.Hour), store.AggScope{})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]float64{
		"dev":     (3 * time.Minute).Seconds(),
		"staging": (12 * time.Minute).Seconds(),
		"prod":    (2*time.Hour + 5*time.Minute).Seconds(),
	}
	if len(lt) != len(want) {
		t.Fatalf("got %d env groups, want %d: %+v", len(lt), len(want), lt)
	}
	for env, secs := range want {
		g := leadGroup(lt, env)
		if g == nil {
			t.Fatalf("missing lead-time group for %s", env)
		}
		if g.Samples != 1 {
			t.Errorf("%s samples = %d, want 1", env, g.Samples)
		}
		if g.MedianSeconds == nil || *g.MedianSeconds != secs {
			t.Errorf("%s median = %v, want %v", env, g.MedianSeconds, secs)
		}
		// One sample: p90 equals the median.
		if g.P90Seconds == nil || *g.P90Seconds != secs {
			t.Errorf("%s p90 = %v, want %v", env, g.P90Seconds, secs)
		}
	}
}

// A change with no build/merge/push in the window (only a deploy) can't be
// timed, so it yields no lead-time sample.
func TestLeadTimeNoIntent(t *testing.T) {
	st := seed(t)
	// demo-web only ever has deploys in the seed (no build/merge carrying its
	// sha), so it never appears in the lead-time output.
	lt, err := leadTime(context.Background(), st, resolver(t), t0.Add(-time.Hour), t0.Add(4*time.Hour), store.AggScope{Services: []string{"demo-web"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(lt) != 0 {
		t.Errorf("demo-web has no timeable change, got %+v", lt)
	}
}

// The env scope narrows which envs report lead time.
func TestLeadTimeScopedEnv(t *testing.T) {
	st := seed(t)
	lt, err := leadTime(context.Background(), st, resolver(t), t0.Add(-time.Hour), t0.Add(4*time.Hour), store.AggScope{Envs: []string{"prod"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(lt) != 1 || lt[0].Env != "prod" {
		t.Fatalf("env=prod scope => %+v, want prod only", lt)
	}
}

func TestPercentile(t *testing.T) {
	xs := []float64{10, 20, 30, 40, 50}
	if got := percentile(xs, 0.5); got != 30 {
		t.Errorf("median = %v, want 30", got)
	}
	if got := percentile(xs, 0.9); got != 46 { // between 40 and 50, 0.9*(4)=3.6
		t.Errorf("p90 = %v, want 46", got)
	}
	if got := percentile(nil, 0.5); got != 0 {
		t.Errorf("empty = %v, want 0", got)
	}
	if got := percentile([]float64{7}, 0.9); got != 7 {
		t.Errorf("single = %v, want 7", got)
	}
}
