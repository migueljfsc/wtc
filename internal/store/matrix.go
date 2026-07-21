package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// The env matrix (portal "diff visualized"): the current running version of
// every service across a set of environments. "Current" = latest succeeded
// deploy, matching the semantics of `wtc diff`.

// MatrixCell is one service's current deploy in one environment. A missing env
// key in a row means the service is not deployed there.
type MatrixCell struct {
	ID       string    `json:"id"`                 // event id, for deep-linking
	Ref      string    `json:"ref,omitempty"`      // git sha / revision
	Artifact string    `json:"artifact,omitempty"` // registry/app:tag
	TS       time.Time `json:"ts"`                 // deploy time
}

// MatrixRow is one service across all requested envs.
type MatrixRow struct {
	Service string                `json:"service"`
	Cells   map[string]MatrixCell `json:"cells"` // keyed by env
}

// Matrix is the services × environments grid.
type Matrix struct {
	Envs     []string    `json:"envs"` // column order as requested
	Services []MatrixRow `json:"services"`
}

// Matrix returns the current-deploy grid for envs (order preserved). When envs
// is empty it defaults to the distinct non-ephemeral (not pr-*) environments,
// alphabetical. A non-zero asOf reconstructs the grid as it stood at that
// instant (point-in-time state); the zero value means "now".
func (s *Store) Matrix(ctx context.Context, envs []string, asOf time.Time) (*Matrix, error) {
	if len(envs) == 0 {
		var err error
		if envs, err = s.defaultMatrixEnvs(ctx, asOf); err != nil {
			return nil, err
		}
	}

	latest, err := s.LatestSucceededDeploys(ctx, envs, asOf)
	if err != nil {
		return nil, err
	}

	rows := map[string]*MatrixRow{}
	for i := range latest {
		ev := latest[i]
		row := rows[ev.Service]
		if row == nil {
			row = &MatrixRow{Service: ev.Service, Cells: map[string]MatrixCell{}}
			rows[ev.Service] = row
		}
		row.Cells[ev.Env] = MatrixCell{
			ID:       ev.ID,
			Ref:      ev.Ref,
			Artifact: ev.Artifact,
			TS:       ev.TS,
		}
	}

	out := make([]MatrixRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	// Never return a nil envs slice (JSON null); an empty ledger => empty grid.
	if envs == nil {
		envs = []string{}
	}
	return &Matrix{Envs: envs, Services: out}, nil
}

// defaultMatrixEnvs lists the distinct mapped, non-ephemeral environments. A
// non-zero asOf excludes envs whose activity all postdates that instant, so a
// point-in-time grid never invents a column for an env that did not yet exist.
func (s *Store) defaultMatrixEnvs(ctx context.Context, asOf time.Time) ([]string, error) {
	q := `SELECT DISTINCT env FROM events
		 WHERE env != '' AND env NOT LIKE 'pr-%' ESCAPE '\'`
	args := []any{}
	if !asOf.IsZero() {
		q += ` AND ts <= ?`
		args = append(args, model.FormatTS(asOf))
	}
	q += ` ORDER BY env LIMIT ?`
	args = append(args, maxFacetValues)
	rows, err := s.readDB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("matrix envs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, fmt.Errorf("matrix envs scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
