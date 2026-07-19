package store

import (
	"context"
	"errors"
	"fmt"
)

// ErrBackupUnsupported marks a snapshot request against a non-sqlite backend.
// Postgres deployments back up with pg_dump / managed snapshots — out of
// wtc's hands (docs/setup/backup.md).
var ErrBackupUnsupported = errors.New("backup: only the sqlite backend supports snapshots")

// BackupInto writes a consistent point-in-time snapshot of the sqlite ledger
// to path via VACUUM INTO — WAL-safe while serving, and the copy comes out
// compacted. Runs on the read pool so the single writer is never blocked.
// path must not already exist (sqlite refuses to overwrite).
func (s *Store) BackupInto(ctx context.Context, path string) error {
	if s.dialect != dialectSQLite {
		return ErrBackupUnsupported
	}
	if _, err := s.readDB.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return nil
}
