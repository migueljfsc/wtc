package store

import (
	"embed"
	"fmt"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationsFS embed.FS

// migrationsDir returns the embedded per-dialect migration directory. The two
// dialects have independent sequences (postgres has no FTS migration); both
// follow the same append-only rule.
func migrationsDir(d dialect) string {
	if d == dialectPostgres {
		return "migrations/postgres"
	}
	return "migrations/sqlite"
}

// migrate applies embedded sequential migrations that have not run yet.
// Files are named NNNN_description.sql and applied in numeric order, each in
// its own transaction, recorded in schema_migrations.
func migrate(db *dbConn, d dialect) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	dir := migrationsDir(d)
	// fs.ReadDir guarantees entries sorted by filename, so NNNN_ prefixes
	// already give numeric application order.
	entries, err := migrationsFS.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		if version <= current {
			continue
		}
		body, err := migrationsFS.ReadFile(dir + "/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if err := applyMigration(db, version, name, string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}

func migrationVersion(name string) (int, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("migration %s: name must be NNNN_description.sql", name)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("migration %s: bad version prefix: %w", name, err)
	}
	return version, nil
}

func applyMigration(db *dbConn, version int, name, body string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(body); err != nil {
		return err
	}
	// tx is a raw *sql.Tx — rebind the recording insert explicitly.
	if _, err := tx.Exec(
		db.rebind(`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`),
		version, name, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return err
	}
	return tx.Commit()
}
