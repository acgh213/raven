// Package db provides SQLite connection setup, embedded migrations, and
// online backup. It uses the pure-Go modernc.org/sqlite driver.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// defaultBusyTimeout is the SQLite busy timeout applied to all connections.
const defaultBusyTimeout = 5 * time.Second

// Open opens a SQLite database at path, enables foreign keys, sets a busy
// timeout, configures WAL journal mode for file-backed databases, and
// validates the connection.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}

	// Foreign keys must be enabled on every connection.
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	// Busy timeout: millisecond precision.
	ms := defaultBusyTimeout.Milliseconds()
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", ms)); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}

	// WAL journal mode for file-backed databases.
	if path != ":memory:" {
		var mode string
		if err := db.QueryRow("PRAGMA journal_mode = WAL").Scan(&mode); err != nil {
			db.Close()
			return nil, fmt.Errorf("enable WAL: %w", err)
		}
		if !strings.EqualFold(mode, "wal") {
			db.Close()
			return nil, fmt.Errorf("expected WAL journal mode, got %q", mode)
		}
	}

	// Validate the connection is alive.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	return db, nil
}

// Migrate applies all embedded SQL migrations in filename order. Each
// migration runs in its own transaction. Already-applied migrations are
// skipped. The _migrations ledger table is created first if it does not
// exist.
func Migrate(ctx context.Context, database *sql.DB) error {
	// Ensure the migrations ledger exists.
	if _, err := database.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS _migrations (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			filename   TEXT NOT NULL UNIQUE,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		)`); err != nil {
		return fmt.Errorf("create _migrations: %w", err)
	}

	// List migration files in sorted order.
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var filenames []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			filenames = append(filenames, e.Name())
		}
	}
	sort.Strings(filenames)

	// Query already-applied migrations.
	applied := make(map[string]bool)
	rows, err := database.QueryContext(ctx, "SELECT filename FROM _migrations")
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan migration name: %w", err)
		}
		applied[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate migrations: %w", err)
	}

	// Apply each unapplied migration in a transaction.
	for _, name := range filenames {
		if applied[name] {
			continue
		}

		content, err := migrationsFS.ReadFile(filepath.Join("migrations", name))
		if err != nil {
			return fmt.Errorf("read migration %q: %w", name, err)
		}

		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for %q: %w", name, err)
		}

		// Apply the migration SQL.
		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %q: %w", name, err)
		}

		// Record the migration.
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO _migrations (filename) VALUES (?)", name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %q: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %q: %w", name, err)
		}
	}

	return nil
}

// Backup creates a consistent SQLite online backup at destination. It uses
// SQLite's VACUUM INTO, which creates a transactionally consistent copy of
// the database. Backup refuses to overwrite an existing nonempty destination.
func Backup(ctx context.Context, database *sql.DB, destination string) error {
	// Refuse to overwrite a nonempty destination.
	info, err := os.Stat(destination)
	if err == nil {
		if info.Size() > 0 {
			return fmt.Errorf("destination %q already exists and is nonempty", destination)
		}
		// File exists but is empty — that's acceptable; we'll overwrite.
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat destination %q: %w", destination, err)
	}

	// VACUUM INTO creates a consistent snapshot. The path must be quoted
	// safely: escape single quotes by doubling them.
	quoted := strings.ReplaceAll(destination, "'", "''")
	sql := fmt.Sprintf("VACUUM INTO '%s'", quoted)

	if _, err := database.ExecContext(ctx, sql); err != nil {
		return fmt.Errorf("backup to %q: %w", destination, err)
	}

	return nil
}
