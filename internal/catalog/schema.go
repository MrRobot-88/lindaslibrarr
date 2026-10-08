package catalog

import (
	"context"
	"database/sql"
	"fmt"
)

// Migrate creates the shared catalog/profile model. Catalog ownership is global;
// reactions, progress and recommendations are profile scoped.
func Migrate(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS catalog_books (
			id INTEGER PRIMARY KEY,
			canonical_key TEXT NOT NULL UNIQUE,
			title TEXT NOT NULL,
			author TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			cover_url TEXT NOT NULL DEFAULT '',
			year INTEGER,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS catalog_editions (
			id INTEGER PRIMARY KEY,
			book_id INTEGER NOT NULL REFERENCES catalog_books(id) ON DELETE CASCADE,
			isbn10 TEXT,
			isbn13 TEXT,
			language TEXT NOT NULL DEFAULT '',
			publisher TEXT NOT NULL DEFAULT '',
			UNIQUE(book_id, isbn13, language)
		)`,
		`CREATE TABLE IF NOT EXISTS catalog_releases (
			id INTEGER PRIMARY KEY,
			book_id INTEGER NOT NULL REFERENCES catalog_books(id) ON DELETE CASCADE,
			edition_id INTEGER REFERENCES catalog_editions(id) ON DELETE SET NULL,
			source TEXT NOT NULL,
			source_id TEXT NOT NULL,
			md5 TEXT,
			format TEXT NOT NULL DEFAULT '',
			language TEXT NOT NULL DEFAULT '',
			size_bytes INTEGER NOT NULL DEFAULT 0,
			available INTEGER NOT NULL DEFAULT 1,
			last_seen_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(source, source_id)
		)`,
		`CREATE TABLE IF NOT EXISTS catalog_library_files (
			id INTEGER PRIMARY KEY,
			book_id INTEGER NOT NULL REFERENCES catalog_books(id) ON DELETE CASCADE,
			edition_id INTEGER REFERENCES catalog_editions(id) ON DELETE SET NULL,
			path TEXT NOT NULL UNIQUE,
			language TEXT NOT NULL DEFAULT '',
			format TEXT NOT NULL DEFAULT '',
			content_hash TEXT,
			added_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_library_content_hash ON catalog_library_files(content_hash) WHERE content_hash IS NOT NULL AND content_hash <> ''`,
		`CREATE TABLE IF NOT EXISTS catalog_profiles (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS catalog_profile_reactions (
			profile_id INTEGER NOT NULL REFERENCES catalog_profiles(id) ON DELETE CASCADE,
			book_id INTEGER NOT NULL REFERENCES catalog_books(id) ON DELETE CASCADE,
			reaction INTEGER NOT NULL CHECK(reaction IN (-1, 1)),
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(profile_id, book_id)
		)`,
		`CREATE TABLE IF NOT EXISTS catalog_profile_progress (
			profile_id INTEGER NOT NULL REFERENCES catalog_profiles(id) ON DELETE CASCADE,
			book_id INTEGER NOT NULL REFERENCES catalog_books(id) ON DELETE CASCADE,
			position REAL NOT NULL DEFAULT 0,
			completed INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(profile_id, book_id)
		)`,
		`CREATE TABLE IF NOT EXISTS catalog_profile_favorites (
			profile_id INTEGER NOT NULL REFERENCES catalog_profiles(id) ON DELETE CASCADE,
			book_id INTEGER NOT NULL REFERENCES catalog_books(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(profile_id, book_id)
		)`,
		`CREATE TABLE IF NOT EXISTS catalog_recommendations (
			profile_id INTEGER NOT NULL REFERENCES catalog_profiles(id) ON DELETE CASCADE,
			book_id INTEGER NOT NULL REFERENCES catalog_books(id) ON DELETE CASCADE,
			score REAL NOT NULL DEFAULT 0,
			reason TEXT NOT NULL DEFAULT '',
			generated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(profile_id, book_id)
		)`,
		`CREATE TABLE IF NOT EXISTS catalog_sync_state (
			source TEXT PRIMARY KEY,
			base TEXT NOT NULL DEFAULT '',
			started_at TEXT,
			completed_at TEXT,
			status TEXT NOT NULL DEFAULT 'never',
			records_seen INTEGER NOT NULL DEFAULT 0,
			records_written INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_catalog_books_title ON catalog_books(title)`,
		`CREATE INDEX IF NOT EXISTS idx_catalog_books_author ON catalog_books(author)`,
		`CREATE INDEX IF NOT EXISTS idx_catalog_releases_book ON catalog_releases(book_id, language, format, available)`,
	}
	for i, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("catalog migration %d: %w", i+1, err)
		}
	}
	return nil
}
