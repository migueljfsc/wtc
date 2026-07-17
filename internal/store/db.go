package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// dialect selects the SQL variant. Every query in this package is written in
// sqlite form with `?` placeholders; dbConn transparently rebinds to $n for
// postgres, and the handful of genuinely divergent queries (FTS, julianday,
// GLOB, pragma sizes) branch on Store.dialect explicitly.
type dialect int

const (
	dialectSQLite dialect = iota
	dialectPostgres
)

// dbConn wraps *sql.DB so postgres gets `?`→`$n` placeholder rebinding without
// touching any call site. The zero-cost path (sqlite) passes queries through
// untouched.
type dbConn struct {
	*sql.DB
	d dialect
}

func (c *dbConn) rebind(query string) string {
	if c.d != dialectPostgres {
		return query
	}
	return rebind(query)
}

// rebind rewrites `?` placeholders as `$1..$n`, skipping single-quoted string
// literals (none of our queries put ? inside one, but cheap insurance).
func rebind(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	inString := false
	for i := 0; i < len(query); i++ {
		ch := query[i]
		switch {
		case ch == '\'':
			inString = !inString
			b.WriteByte(ch)
		case ch == '?' && !inString:
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

func (c *dbConn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.DB.QueryContext(ctx, c.rebind(query), args...)
}

func (c *dbConn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.DB.QueryRowContext(ctx, c.rebind(query), args...)
}

func (c *dbConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.DB.ExecContext(ctx, c.rebind(query), args...)
}

func (c *dbConn) Prepare(query string) (*sql.Stmt, error) {
	return c.DB.Prepare(c.rebind(query))
}
