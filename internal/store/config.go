package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
)

// Config overrides (P10): DB-backed editable normalization config. Values are
// opaque JSON; the server owns the schema. Reads use the read pool; writes go
// to the single write connection (MaxOpenConns=1), so they serialize with the
// event writer at the connection level — safe for the infrequent config edit.

// GetConfigOverride returns the stored JSON for key. ok is false when no
// override is set (the caller falls back to the YAML value).
func (s *Store) GetConfigOverride(ctx context.Context, key string) (value string, ok bool, err error) {
	err = s.readDB.QueryRowContext(ctx,
		`SELECT value FROM config_overrides WHERE key = ?`, key).Scan(&value)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("get config override %q: %w", key, err)
	}
	return value, true, nil
}

// SetConfigOverride upserts the JSON value for key.
func (s *Store) SetConfigOverride(ctx context.Context, key, value string) error {
	_, err := s.writeDB.ExecContext(ctx, `
INSERT INTO config_overrides (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, model.FormatTS(time.Now()))
	if err != nil {
		return fmt.Errorf("set config override %q: %w", key, err)
	}
	return nil
}

// DeleteConfigOverride removes key's override, resetting it to the YAML value.
func (s *Store) DeleteConfigOverride(ctx context.Context, key string) error {
	if _, err := s.writeDB.ExecContext(ctx,
		`DELETE FROM config_overrides WHERE key = ?`, key); err != nil {
		return fmt.Errorf("delete config override %q: %w", key, err)
	}
	return nil
}
