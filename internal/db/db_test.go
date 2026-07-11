package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// requiredTables lists all tables the Phase 1 migration must create.
var requiredTables = []string{
	"feeds",
	"articles",
	"article_content_versions",
	"activity_events",
	"article_state",
	"jobs",
	"_migrations",
}

// TestFreshMigration creates a fresh database, runs Migrate, and verifies:
//   - all named tables exist
//   - exactly one migration record is in _migrations
func TestFreshMigration(t *testing.T) {
	t.Parallel()

	db := openMemory(t)
	ctx := context.Background()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Check all tables exist.
	for _, name := range requiredTables {
		if !tableExists(t, db, name) {
			t.Errorf("expected table %q to exist after migration", name)
		}
	}

	// Check exactly one migration record.
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM _migrations").Scan(&count); err != nil {
		t.Fatalf("query _migrations count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 migration record, got %d", count)
	}
}

// TestReRunMigration verifies that calling Migrate a second time is a no-op:
// it leaves exactly one migration record and does not error.
func TestReRunMigration(t *testing.T) {
	t.Parallel()

	db := openMemory(t)
	ctx := context.Background()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM _migrations").Scan(&count); err != nil {
		t.Fatalf("query _migrations count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 migration record after re-run, got %d", count)
	}
}

// TestForeignKeyEnforcement verifies that foreign-key enforcement is on.
// Inserting a child row with a nonexistent parent must fail.
func TestForeignKeyEnforcement(t *testing.T) {
	t.Parallel()

	db := openMemory(t)
	ctx := context.Background()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Try inserting an article referencing a nonexistent feed.
	_, err := db.ExecContext(ctx,
		`INSERT INTO articles (id, feed_id, guid, title, created_at, updated_at)
		 VALUES ('art-1', 'no-such-feed', 'guid-1', 'test', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`)
	if err == nil {
		t.Fatal("expected foreign-key violation for invalid feed_id, got nil")
	}
}

// TestFileDBConfig verifies that a file-backed database has WAL journal mode
// and a nonzero busy timeout.
func TestFileDBConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Check journal mode is WAL.
	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected journal_mode 'wal', got %q", journalMode)
	}

	// Check busy timeout is nonzero.
	var busyTimeout int
	if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busyTimeout <= 0 {
		t.Errorf("expected busy_timeout > 0, got %d", busyTimeout)
	}
}

// TestBackupRestore verifies that Backup creates a consistent snapshot that can
// be opened and queried, returning rows inserted into the source.
func TestBackupRestore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")

	src, err := Open(srcPath)
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}
	defer src.Close()

	ctx := context.Background()
	if err := Migrate(ctx, src); err != nil {
		t.Fatalf("Migrate source: %v", err)
	}

	// Insert a feed and article.
	if _, err := src.ExecContext(ctx,
		`INSERT INTO feeds (id, feed_url, title, created_at, updated_at)
		 VALUES ('feed-1', 'https://example.com/feed.xml', 'Example Feed',
		         '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	if _, err := src.ExecContext(ctx,
		`INSERT INTO articles (id, feed_id, guid, title, created_at, updated_at)
		 VALUES ('art-1', 'feed-1', 'guid-1', 'Test Article',
		         '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert article: %v", err)
	}

	// Create a backup.
	dstPath := filepath.Join(dir, "backup.db")
	if err := Backup(ctx, src, dstPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// Open the backup independently and verify data.
	dst, err := Open(dstPath)
	if err != nil {
		t.Fatalf("Open backup: %v", err)
	}
	defer dst.Close()

	var feedTitle string
	if err := dst.QueryRowContext(ctx,
		"SELECT title FROM feeds WHERE id = 'feed-1'").Scan(&feedTitle); err != nil {
		t.Fatalf("query backup feed: %v", err)
	}
	if feedTitle != "Example Feed" {
		t.Errorf("backup feed title: got %q, want %q", feedTitle, "Example Feed")
	}

	var articleTitle string
	if err := dst.QueryRowContext(ctx,
		"SELECT title FROM articles WHERE id = 'art-1'").Scan(&articleTitle); err != nil {
		t.Fatalf("query backup article: %v", err)
	}
	if articleTitle != "Test Article" {
		t.Errorf("backup article title: got %q, want %q", articleTitle, "Test Article")
	}
}

// TestBackupRefusesNonemptyDestination verifies that Backup refuses to
// overwrite an existing nonempty destination.
func TestBackupRefusesNonemptyDestination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")

	src, err := Open(srcPath)
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}
	defer src.Close()

	ctx := context.Background()
	if err := Migrate(ctx, src); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Create a nonempty destination file.
	dstPath := filepath.Join(dir, "existing.db")
	if err := os.WriteFile(dstPath, []byte("some data"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	if err := Backup(ctx, src, dstPath); err == nil {
		t.Fatal("expected Backup to refuse nonempty destination, got nil")
	}
}

// TestBackupRefusesEmptyDestination verifies that Backup also refuses to
// overwrite an existing empty destination file.
func TestBackupRefusesEmptyDestination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")

	src, err := Open(srcPath)
	if err != nil {
		t.Fatalf("Open source: %v", err)
	}
	defer src.Close()

	ctx := context.Background()
	if err := Migrate(ctx, src); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Create an empty destination file.
	dstPath := filepath.Join(dir, "empty.db")
	if err := os.WriteFile(dstPath, []byte{}, 0o644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	if err := Backup(ctx, src, dstPath); err == nil {
		t.Fatal("expected Backup to refuse empty destination, got nil")
	}
}

// TestConnPoolConfig verifies that Open configures MaxOpenConns=1 and
// MaxIdleConns=1, and that foreign-key enforcement remains active after
// repeated database operations through the single connection.
func TestConnPoolConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "pool.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Check pool configuration.
	stats := db.Stats()
	if stats.MaxOpenConnections != 1 {
		t.Errorf("expected MaxOpenConnections=1, got %d", stats.MaxOpenConnections)
	}

	ctx := context.Background()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Perform repeated operations through the pool to exercise the
	// single connection.
	for i := 0; i < 10; i++ {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO feeds (id, feed_url, title, created_at, updated_at)
			 VALUES (?, ?, 'Example Feed',
			         '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`,
			fmt.Sprintf("feed-%d", i),
			fmt.Sprintf("https://example.com/feed-%d.xml", i),
		); err != nil {
			t.Fatalf("insert feed iteration %d: %v", i, err)
		}
	}

	// Foreign key enforcement must still be active after repeated use.
	_, err = db.ExecContext(ctx,
		`INSERT INTO articles (id, feed_id, guid, title, created_at, updated_at)
		 VALUES ('art-bad', 'no-such-feed', 'guid-bad', 'test',
		         '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`)
	if err == nil {
		t.Fatal("expected foreign-key violation for invalid feed_id after repeated ops, got nil")
	}
}

// TestLatestContentVersionFK verifies that a deferred foreign key on
// articles.latest_content_version_id prevents referencing a nonexistent
// article_content_versions row when the value is non-null.
func TestLatestContentVersionFK(t *testing.T) {
	t.Parallel()

	db := openMemory(t)
	ctx := context.Background()

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Insert a feed and article (prerequisites).
	if _, err := db.ExecContext(ctx,
		`INSERT INTO feeds (id, feed_url, title, created_at, updated_at)
		 VALUES ('feed-1', 'https://example.com/feed.xml', 'Example Feed',
		         '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO articles (id, feed_id, guid, title, created_at, updated_at)
		 VALUES ('art-1', 'feed-1', 'guid-1', 'Test Article',
		         '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert article: %v", err)
	}

	// Attempting to set latest_content_version_id to a nonexistent version
	// should fail (deferred constraint checked at commit or by PRAGMA).
	_, err := db.ExecContext(ctx,
		`UPDATE articles SET latest_content_version_id = 'no-such-version' WHERE id = 'art-1'`)
	if err == nil {
		t.Fatal("expected FK violation for invalid latest_content_version_id, got nil")
	}

	// Setting it to NULL (the default) must succeed.
	if _, err := db.ExecContext(ctx,
		`UPDATE articles SET latest_content_version_id = NULL WHERE id = 'art-1'`,
	); err != nil {
		t.Fatalf("expected NULL latest_content_version_id to succeed: %v", err)
	}
}

// --- helpers ---

// openMemory opens an in-memory SQLite database using the db.Open helper.
// The caller is responsible for closing it.
func openMemory(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	return db
}

// tableExists reports whether a table or view with the given name exists in
// the sqlite_schema table.
func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name,
	).Scan(&count)
	if err != nil {
		t.Fatalf("check table %q: %v", name, err)
	}
	return count > 0
}
