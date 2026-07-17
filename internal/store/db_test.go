package store

import "testing"

func TestRebind(t *testing.T) {
	cases := []struct{ in, want string }{
		{"SELECT 1", "SELECT 1"},
		{"SELECT * FROM t WHERE a = ? AND b = ?", "SELECT * FROM t WHERE a = $1 AND b = $2"},
		{"INSERT INTO t VALUES (?, ?, ?)", "INSERT INTO t VALUES ($1, $2, $3)"},
		// A ? inside a string literal must survive untouched.
		{"SELECT '?' , x FROM t WHERE y = ?", "SELECT '?' , x FROM t WHERE y = $1"},
		{"WHERE env NOT LIKE 'pr-%' ESCAPE '\\' AND ts < ?", "WHERE env NOT LIKE 'pr-%' ESCAPE '\\' AND ts < $1"},
	}
	for _, c := range cases {
		if got := rebind(c.in); got != c.want {
			t.Errorf("rebind(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
