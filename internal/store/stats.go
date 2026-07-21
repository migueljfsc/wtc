package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// Dashboard aggregations (DORA-ish portal metrics). All grouping is
// done in SQL over the events table; ts is stored as sortable fixed-width UTC
// ISO-8601 text, so substr slices the bucket label directly (portable across
// sqlite and postgres). Buckets are UTC — the client formats to local for
// display.

// maxBuckets caps a single activity request so an hour-bucketed multi-year
// window can't allocate an unbounded slice.
const maxBuckets = 2000

// ActivityBucket is one time slice of overall event activity.
type ActivityBucket struct {
	TS        string `json:"ts"`        // bucket start: "2006-01-02" (day) or "2006-01-02T15:00" (hour), UTC
	Total     int    `json:"total"`     // all events in the slice
	Succeeded int    `json:"succeeded"` // status = succeeded
	Failed    int    `json:"failed"`    // status = failed
}

// ActivityStats is the events-over-time series for the dashboard.
type ActivityStats struct {
	Since   time.Time        `json:"since"`
	Until   time.Time        `json:"until"`
	Bucket  string           `json:"bucket"` // "day" | "hour"
	Buckets []ActivityBucket `json:"buckets"`
}

type bucketSpec struct {
	sqlExpr string // SQL expression producing the bucket label from ts
	goFmt   string // matching Go layout
	step    time.Duration
}

// bucketSpecFor builds the bucket label straight from the stored text: ts is
// canonical fixed-width UTC ("2006-01-02T15:04:05.000Z"), so substr slices the
// day/hour prefix without any datetime function — portable across sqlite and
// postgres, replacing the sqlite-only strftime.
func bucketSpecFor(bucket string) (bucketSpec, error) {
	switch bucket {
	case "day":
		return bucketSpec{sqlExpr: "substr(ts, 1, 10)", goFmt: "2006-01-02", step: 24 * time.Hour}, nil
	case "hour":
		return bucketSpec{sqlExpr: "substr(ts, 1, 13) || ':00'", goFmt: "2006-01-02T15:00", step: time.Hour}, nil
	default:
		return bucketSpec{}, fmt.Errorf("invalid bucket %q: want day or hour", bucket)
	}
}

// ActivityStats returns a contiguous (gap-filled) event-count series in
// [since, until) at day or hour granularity.
func (s *Store) ActivityStats(ctx context.Context, since, until time.Time, bucket string) (*ActivityStats, error) {
	spec, err := bucketSpecFor(bucket)
	if err != nil {
		return nil, err
	}
	since, until = since.UTC(), until.UTC()
	if !until.After(since) {
		return &ActivityStats{Since: since, Until: until, Bucket: bucket, Buckets: []ActivityBucket{}}, nil
	}
	// Guard against an unbounded gap-fill (e.g. hour buckets over years).
	start := since.Truncate(spec.step)
	if until.Sub(start)/spec.step > maxBuckets {
		return nil, fmt.Errorf("window too large for %s buckets (max %d): narrow the range", bucket, maxBuckets)
	}

	rows, err := s.readDB.QueryContext(ctx, `
SELECT `+spec.sqlExpr+` AS bucket,
       COUNT(*),
       SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END),
       SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END)
FROM events
WHERE ts >= ? AND ts <= ?
GROUP BY bucket`, model.FormatTS(since), model.FormatTS(until))
	if err != nil {
		return nil, fmt.Errorf("activity stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := map[string]ActivityBucket{}
	for rows.Next() {
		var b ActivityBucket
		if err := rows.Scan(&b.TS, &b.Total, &b.Succeeded, &b.Failed); err != nil {
			return nil, fmt.Errorf("activity stats scan: %w", err)
		}
		counts[b.TS] = b
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("activity stats: %w", err)
	}

	// Gap-fill so the chart is contiguous — bucket labels are generated in the
	// same UTC layout strftime produced, so lookups line up.
	buckets := make([]ActivityBucket, 0, int(until.Sub(start)/spec.step)+1)
	for t := start; t.Before(until); t = t.Add(spec.step) {
		label := t.Format(spec.goFmt)
		if b, ok := counts[label]; ok {
			buckets = append(buckets, b)
		} else {
			buckets = append(buckets, ActivityBucket{TS: label})
		}
	}
	return &ActivityStats{Since: since, Until: until, Bucket: bucket, Buckets: buckets}, nil
}

// EnvDeployStats is per-environment deploy activity for the dashboard's
// frequency/failure tiles and env-health cards.
type EnvDeployStats struct {
	Env        string     `json:"env"`
	Total      int        `json:"total"` // deploy events in the window
	Succeeded  int        `json:"succeeded"`
	Failed     int        `json:"failed"`
	Services   int        `json:"services"`          // distinct services deployed
	LastTS     *time.Time `json:"last_ts,omitempty"` // most recent deploy
	LastStatus string     `json:"last_status,omitempty"`
}

// DeployStats is the per-env deploy summary over a window.
type DeployStats struct {
	Since time.Time        `json:"since"`
	Until time.Time        `json:"until"`
	Envs  []EnvDeployStats `json:"envs"`
}

// DeployStats aggregates deploy events per (mapped) environment in
// [since, until). Unmapped (env="") deploys are excluded — the dashboard is
// per-environment; env="" is surfaced by doctor instead.
func (s *Store) DeployStats(ctx context.Context, since, until time.Time) (*DeployStats, error) {
	since, until = since.UTC(), until.UTC()
	rows, err := s.readDB.QueryContext(ctx, `
SELECT env, status, ts, service
FROM events
WHERE kind = 'deploy' AND env != '' AND ts >= ? AND ts <= ?
ORDER BY ts DESC`, model.FormatTS(since), model.FormatTS(until))
	if err != nil {
		return nil, fmt.Errorf("deploy stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type acc struct {
		st       EnvDeployStats
		services map[string]struct{}
	}
	byEnv := map[string]*acc{}
	for rows.Next() {
		var env, status, tsStr, service string
		if err := rows.Scan(&env, &status, &tsStr, &service); err != nil {
			return nil, fmt.Errorf("deploy stats scan: %w", err)
		}
		a := byEnv[env]
		if a == nil {
			a = &acc{st: EnvDeployStats{Env: env}, services: map[string]struct{}{}}
			byEnv[env] = a
		}
		a.st.Total++
		switch model.Status(status) {
		case model.StatusSucceeded:
			a.st.Succeeded++
		case model.StatusFailed:
			a.st.Failed++
		}
		if service != "" {
			a.services[service] = struct{}{}
		}
		// Rows arrive newest-first, so the first one seen per env is the latest.
		if a.st.LastTS == nil {
			if ts, err := model.ParseTS(tsStr); err == nil {
				a.st.LastTS = &ts
				a.st.LastStatus = status
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("deploy stats: %w", err)
	}

	envs := make([]EnvDeployStats, 0, len(byEnv))
	for _, a := range byEnv {
		a.st.Services = len(a.services)
		envs = append(envs, a.st)
	}
	sort.Slice(envs, func(i, j int) bool { return envs[i].Env < envs[j].Env })
	return &DeployStats{Since: since, Until: until, Envs: envs}, nil
}

// Facets are the distinct dimension values present in the ledger, for the
// timeline's filter dropdowns. Kind and status are fixed enums (the client
// knows them); env/service/actor are dynamic.
type Facets struct {
	Sources  []string `json:"sources"`
	Envs     []string `json:"envs"`
	Services []string `json:"services"`
	Repos    []string `json:"repos"`
	Owners   []string `json:"owners"`
	Actors   []string `json:"actors"`
}

// maxFacetValues caps each dimension so a high-cardinality column (many
// actors/ephemeral envs) can't return an unbounded list.
const maxFacetValues = 500

// Facets returns the sorted distinct non-empty source/env/service/repo/owner/actor values.
func (s *Store) Facets(ctx context.Context) (*Facets, error) {
	distinct := func(col string) ([]string, error) {
		// col is a fixed literal from the call sites below, never user input.
		rows, err := s.readDB.QueryContext(ctx,
			`SELECT DISTINCT `+col+` FROM events WHERE `+col+` != '' ORDER BY `+col+` LIMIT ?`,
			maxFacetValues)
		if err != nil {
			return nil, fmt.Errorf("facets %s: %w", col, err)
		}
		defer func() { _ = rows.Close() }()
		out := []string{} // marshals as [], never null — clients index facet lists
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				return nil, fmt.Errorf("facets %s scan: %w", col, err)
			}
			out = append(out, v)
		}
		return out, rows.Err()
	}

	f := &Facets{}
	var err error
	if f.Sources, err = distinct("source"); err != nil {
		return nil, err
	}
	if f.Envs, err = distinct("env"); err != nil {
		return nil, err
	}
	if f.Services, err = distinct("service"); err != nil {
		return nil, err
	}
	if f.Repos, err = distinct("repo"); err != nil {
		return nil, err
	}
	if f.Owners, err = distinct("owner"); err != nil {
		return nil, err
	}
	if f.Actors, err = distinct("actor"); err != nil {
		return nil, err
	}
	return f, nil
}
