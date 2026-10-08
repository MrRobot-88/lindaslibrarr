package catalog

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateCreatesSharedLibraryAndProfileTables(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil { t.Fatal(err) }
	if err := Migrate(context.Background(), db); err != nil { t.Fatal(err) }

	for _, table := range []string{
		"catalog_books", "catalog_editions", "catalog_releases", "catalog_library_files",
		"catalog_profiles", "catalog_profile_reactions", "catalog_profile_progress",
		"catalog_profile_favorites", "catalog_recommendations", "catalog_sync_state",
	} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("missing %s: %v", table, err)
		}
	}
}

func TestReactionIsPerProfileAndLibraryFileIsShared(t *testing.T) {
	db, _ := sql.Open("sqlite", ":memory:")
	defer db.Close()
	db.Exec(`PRAGMA foreign_keys=ON`)
	if err := Migrate(context.Background(), db); err != nil { t.Fatal(err) }

	res, _ := db.Exec(`INSERT INTO catalog_books(canonical_key,title,author) VALUES('isbn:9780000000001','Book','Author')`)
	bookID, _ := res.LastInsertId()
	db.Exec(`INSERT INTO catalog_profiles(name) VALUES('Linda'),('Reader 2')`)
	db.Exec(`INSERT INTO catalog_profile_reactions(profile_id,book_id,reaction) VALUES(1,?,1),(2,?,-1)`, bookID, bookID)
	if _, err := db.Exec(`INSERT INTO catalog_library_files(book_id,path,language,format,content_hash) VALUES(?, '/books/book.epub','sv','epub','same')`, bookID); err != nil { t.Fatal(err) }
	if _, err := db.Exec(`INSERT INTO catalog_library_files(book_id,path,language,format,content_hash) VALUES(?, '/books/copy.epub','sv','epub','same')`, bookID); err == nil {
		t.Fatal("expected duplicate content hash to be rejected")
	}
}
