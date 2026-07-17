package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// RetentionResult reports what a single Retain pass deleted.
type RetentionResult struct {
	DeletedNormal    int64 `json:"deleted_normal"`
	DeletedEphemeral int64 `json:"deleted_ephemeral"`
}

// Retain deletes events past their retention cutoff, then reclaims freed pages
// via incremental_vacuum (auto_vacuum=INCREMENTAL is set at Open). Rows whose
// env matches ephemeralPattern (a SQLite GLOB such as "pr-*") use ephemeralKeep;
// everything else — including unmapped env="" rows — uses keep. A zero
// ephemeralKeep falls back to keep; an empty pattern disables the ephemeral arm.
//
// On sqlite the FTS index stays consistent automatically: the events_fts_ad
// AFTER DELETE trigger removes each deleted row from the index. (Postgres has
// no FTS index — search is ILIKE.)
//
// Runs on writeDB, which is capped at one connection (SetMaxOpenConns(1)), so
// it serializes with the single writer goroutine — no two statements ever hit
// the database concurrently and there is no SQLITE_BUSY to fight.
func (s *Store) Retain(ctx context.Context, now time.Time, keep, ephemeralKeep time.Duration, ephemeralPattern string) (RetentionResult, error) {
	var res RetentionResult
	if keep <= 0 {
		return res, fmt.Errorf("retain: keep must be positive, got %s", keep)
	}

	normalCutoff := model.FormatTS(now.Add(-keep))

	// The ephemeral pattern is documented as a SQLite GLOB ("pr-*"). On
	// postgres the same pattern is translated to an anchored regex and matched
	// with ~ (case-sensitive, like GLOB).
	globCond, globArg := "env GLOB ?", func(p string) string { return p }
	if s.dialect == dialectPostgres {
		globCond, globArg = "env ~ ?", globToRegex
	}

	// Ephemeral arm first: prune pr-* rows past their (shorter) cutoff. The
	// normal arm below explicitly excludes the pattern, so an ephemeral row can
	// never be kept longer than ephemeralKeep just because keep is larger.
	if ephemeralPattern != "" {
		ek := ephemeralKeep
		if ek <= 0 {
			ek = keep
		}
		ephCutoff := model.FormatTS(now.Add(-ek))
		r, err := s.writeDB.ExecContext(ctx,
			`DELETE FROM events WHERE `+globCond+` AND ts < ?`, globArg(ephemeralPattern), ephCutoff)
		if err != nil {
			return res, fmt.Errorf("retain: ephemeral delete: %w", err)
		}
		res.DeletedEphemeral, _ = r.RowsAffected()
	}

	// Normal arm: everything the ephemeral pattern does not match.
	var (
		r   sql.Result
		err error
	)
	if ephemeralPattern != "" {
		r, err = s.writeDB.ExecContext(ctx,
			`DELETE FROM events WHERE NOT (`+globCond+`) AND ts < ?`, globArg(ephemeralPattern), normalCutoff)
	} else {
		r, err = s.writeDB.ExecContext(ctx,
			`DELETE FROM events WHERE ts < ?`, normalCutoff)
	}
	if err != nil {
		return res, fmt.Errorf("retain: delete: %w", err)
	}
	res.DeletedNormal, _ = r.RowsAffected()

	// Reclaim pages freed by the deletes above (sqlite only — postgres
	// autovacuum handles reclamation). A no-op when nothing was freed.
	if s.dialect == dialectSQLite {
		if _, err := s.writeDB.ExecContext(ctx, `PRAGMA incremental_vacuum`); err != nil {
			return res, fmt.Errorf("retain: incremental_vacuum: %w", err)
		}
	}
	return res, nil
}

// globToRegex translates a SQLite GLOB pattern (* any run, ? one char) into an
// anchored regex for postgres's ~ operator. Character classes ([...]) are not
// supported — the retention docs advertise * and ? only.
func globToRegex(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return b.String()
}
