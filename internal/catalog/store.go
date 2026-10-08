package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var PreferredLanguages = []string{"sv", "en", "da", "es"}

type Release struct {
	ID        int64  `json:"id"`
	BookID    int64  `json:"book_id"`
	Source    string `json:"source"`
	SourceID  string `json:"source_id"`
	MD5       string `json:"md5,omitempty"`
	Format    string `json:"format"`
	Language  string `json:"language"`
	SizeBytes int64  `json:"size_bytes"`
	Available bool   `json:"available"`
}

type Book struct {
	ID           int64  `json:"id"`
	CanonicalKey string `json:"canonical_key"`
	Title        string `json:"title"`
	Author       string `json:"author"`
	Description  string `json:"description,omitempty"`
	CoverURL     string `json:"cover_url,omitempty"`
	Year         int    `json:"year,omitempty"`
	InLibrary    bool   `json:"in_library"`
}

type Store struct{ DB *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{DB: db} }

func (s *Store) SearchBooks(ctx context.Context, query string, limit int) ([]Book, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	q := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	rows, err := s.DB.QueryContext(ctx, `
		SELECT b.id,b.canonical_key,b.title,b.author,b.description,b.cover_url,COALESCE(b.year,0),
		EXISTS(SELECT 1 FROM catalog_library_files f WHERE f.book_id=b.id)
		FROM catalog_books b
		WHERE lower(b.title) LIKE ? OR lower(b.author) LIKE ?
		ORDER BY CASE WHEN lower(b.title)=lower(?) THEN 0 WHEN lower(b.title) LIKE lower(?) THEN 1 ELSE 2 END,
		b.title LIMIT ?`, q, q, strings.TrimSpace(query), strings.TrimSpace(query)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(&b.ID, &b.CanonicalKey, &b.Title, &b.Author, &b.Description, &b.CoverURL, &b.Year, &b.InLibrary); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) SetReaction(ctx context.Context, profileID, bookID int64, reaction int) error {
	if reaction != -1 && reaction != 1 {
		return errors.New("reaction must be -1 or 1")
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO catalog_profile_reactions(profile_id,book_id,reaction,updated_at)
		VALUES(?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(profile_id,book_id) DO UPDATE SET reaction=excluded.reaction,updated_at=CURRENT_TIMESTAMP`, profileID, bookID, reaction)
	return err
}

func (s *Store) BestRelease(ctx context.Context, bookID int64, requestedLanguage string) (*Release, error) {
	langs := PreferredLanguages
	if requestedLanguage != "" {
		langs = append([]string{requestedLanguage}, PreferredLanguages...)
	}
	caseSQL := "CASE"
	args := []any{bookID}
	seen := map[string]bool{}
	rank := 0
	for _, lang := range langs {
		lang = strings.ToLower(strings.TrimSpace(lang))
		if lang == "" || seen[lang] {
			continue
		}
		seen[lang] = true
		caseSQL += fmt.Sprintf(" WHEN lower(language)=? THEN %d", rank)
		args = append(args, lang)
		rank++
	}
	caseSQL += " ELSE 100 END"
	query := `SELECT id,book_id,source,source_id,COALESCE(md5,''),format,language,size_bytes,available
		FROM catalog_releases WHERE book_id=? AND available=1 ORDER BY ` + caseSQL + `,
		CASE lower(format) WHEN 'epub' THEN 0 WHEN 'pdf' THEN 1 ELSE 2 END, size_bytes DESC LIMIT 1`
	row := s.DB.QueryRowContext(ctx, query, args...)
	var r Release
	if err := row.Scan(&r.ID, &r.BookID, &r.Source, &r.SourceID, &r.MD5, &r.Format, &r.Language, &r.SizeBytes, &r.Available); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}
