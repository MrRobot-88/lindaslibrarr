package catalog

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

const maxAnnaRecordBytes = 16 << 20

var nonISBN = regexp.MustCompile(`[^0-9Xx]`)

type AnnaRecord struct {
	Index  string           `json:"_index"`
	ID     string           `json:"_id"`
	Source AnnaRecordSource `json:"_source"`
}

type AnnaRecordSource struct {
	ID               string               `json:"id"`
	FileUnifiedData  AnnaFileUnifiedData  `json:"file_unified_data"`
	SearchOnlyFields AnnaSearchOnlyFields `json:"search_only_fields"`
}

type AnnaFileUnifiedData struct {
	CoverURLBest            string              `json:"cover_url_best"`
	ExtensionBest           string              `json:"extension_best"`
	FilesizeBest            int64               `json:"filesize_best"`
	TitleBest               string              `json:"title_best"`
	AuthorBest              string              `json:"author_best"`
	PublisherBest           string              `json:"publisher_best"`
	YearBest                string              `json:"year_best"`
	LanguageCodes           []string            `json:"language_codes"`
	ContentTypeBest         string              `json:"content_type_best"`
	StrippedDescriptionBest string              `json:"stripped_description_best"`
	IdentifiersUnified      map[string][]string `json:"identifiers_unified"`
	ClassificationsUnified  map[string][]string `json:"classifications_unified"`
}

type AnnaSearchOnlyFields struct {
	SearchFilesize    int64    `json:"search_filesize"`
	SearchYear        string   `json:"search_year"`
	SearchExtension   string   `json:"search_extension"`
	SearchContentType string   `json:"search_content_type"`
	SearchISBN13      []string `json:"search_isbn13"`
	SearchTitle       string   `json:"search_title"`
	SearchAuthor      string   `json:"search_author"`
	SearchPublisher   string   `json:"search_publisher"`
	SearchAddedDate   string   `json:"search_added_date"`
}

type AnnaCandidate struct {
	CanonicalKey   string
	SourceID       string
	MD5            string
	Title          string
	Author         string
	Publisher      string
	Description    string
	CoverURL       string
	Year           int
	Format         string
	SizeBytes      int64
	Languages      []string
	ISBN10         []string
	ISBN13         []string
	Identifiers    map[string][]string
	Classifications map[string][]string
}

func ParseAnnaJSONL(ctx context.Context, r io.Reader, handle func(AnnaRecord) error) (int64, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), maxAnnaRecordBytes)
	var seen int64
	for s.Scan() {
		if err := ctx.Err(); err != nil {
			return seen, err
		}
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		var record AnnaRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return seen, fmt.Errorf("decode anna record %d: %w", seen+1, err)
		}
		seen++
		if err := handle(record); err != nil {
			return seen, err
		}
	}
	if err := s.Err(); err != nil {
		return seen, fmt.Errorf("read anna metadata: %w", err)
	}
	return seen, nil
}

func ParseAnnaGzipJSONL(ctx context.Context, r io.Reader, handle func(AnnaRecord) error) (int64, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("open anna gzip stream: %w", err)
	}
	defer gz.Close()
	return ParseAnnaJSONL(ctx, gz, handle)
}

func CandidateFromAnna(record AnnaRecord) (AnnaCandidate, bool) {
	f := record.Source.FileUnifiedData
	format := strings.ToLower(strings.TrimSpace(f.ExtensionBest))
	if format == "" {
		format = strings.ToLower(strings.TrimSpace(record.Source.SearchOnlyFields.SearchExtension))
	}
	if format != "epub" {
		return AnnaCandidate{}, false
	}

	title := cleanText(f.TitleBest)
	if title == "" {
		title = cleanText(record.Source.SearchOnlyFields.SearchTitle)
	}
	if title == "" {
		return AnnaCandidate{}, false
	}
	author := cleanText(f.AuthorBest)
	if author == "" {
		author = cleanText(record.Source.SearchOnlyFields.SearchAuthor)
	}
	publisher := cleanText(f.PublisherBest)
	if publisher == "" {
		publisher = cleanText(record.Source.SearchOnlyFields.SearchPublisher)
	}
	year := parseYear(f.YearBest)
	if year == 0 {
		year = parseYear(record.Source.SearchOnlyFields.SearchYear)
	}
	size := f.FilesizeBest
	if size <= 0 {
		size = record.Source.SearchOnlyFields.SearchFilesize
	}

	isbn13 := collectISBN13(f.IdentifiersUnified, record.Source.SearchOnlyFields.SearchISBN13)
	isbn10 := collectIdentifier(f.IdentifiersUnified, "isbn10", 10)
	languages := normalizeLanguages(f.LanguageCodes)
	sourceID := strings.TrimSpace(record.ID)
	if sourceID == "" {
		sourceID = strings.TrimSpace(record.Source.ID)
	}

	return AnnaCandidate{
		CanonicalKey: canonicalKey(title, author, year, isbn13, isbn10),
		SourceID: sourceID,
		MD5: extractMD5(sourceID, f.IdentifiersUnified),
		Title: title,
		Author: author,
		Publisher: publisher,
		Description: cleanText(f.StrippedDescriptionBest),
		CoverURL: strings.TrimSpace(f.CoverURLBest),
		Year: year,
		Format: format,
		SizeBytes: size,
		Languages: languages,
		ISBN10: isbn10,
		ISBN13: isbn13,
		Identifiers: f.IdentifiersUnified,
		Classifications: f.ClassificationsUnified,
	}, true
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimSpace(s)
}

func parseYear(s string) int {
	s = strings.TrimSpace(s)
	if len(s) >= 4 {
		s = s[:4]
	}
	y, err := strconv.Atoi(s)
	if err != nil || y < 0 || y > 9999 {
		return 0
	}
	return y
}

func normalizeLanguages(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, lang := range in {
		lang = strings.ToLower(strings.TrimSpace(lang))
		if lang == "" || seen[lang] {
			continue
		}
		seen[lang] = true
		out = append(out, lang)
	}
	return out
}

func collectISBN13(ids map[string][]string, extra []string) []string {
	vals := append([]string{}, extra...)
	for k, v := range ids {
		key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(k, "_", ""), "-", ""))
		if key == "isbn13" || key == "isbn" {
			vals = append(vals, v...)
		}
	}
	return normalizedISBNs(vals, 13)
}

func collectIdentifier(ids map[string][]string, want string, length int) []string {
	var vals []string
	want = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(want, "_", ""), "-", ""))
	for k, v := range ids {
		key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(k, "_", ""), "-", ""))
		if key == want {
			vals = append(vals, v...)
		}
	}
	return normalizedISBNs(vals, length)
}

func normalizedISBNs(vals []string, length int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(vals))
	for _, value := range vals {
		value = strings.ToUpper(nonISBN.ReplaceAllString(value, ""))
		if len(value) != length || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func canonicalKey(title, author string, year int, isbn13, isbn10 []string) string {
	if len(isbn13) > 0 {
		return "isbn13:" + isbn13[0]
	}
	if len(isbn10) > 0 {
		return "isbn10:" + isbn10[0]
	}
	seed := strings.ToLower(strings.Join(strings.Fields(title+"\x1f"+author), " "))
	if year > 0 {
		seed += fmt.Sprintf("\x1f%d", year)
	}
	h := sha256.Sum256([]byte(seed))
	return "anna-work:" + hex.EncodeToString(h[:])
}

func extractMD5(sourceID string, ids map[string][]string) string {
	for _, value := range ids["md5"] {
		if md5 := normalizeMD5(value); md5 != "" {
			return md5
		}
	}
	if strings.HasPrefix(strings.ToLower(sourceID), "md5:") {
		return normalizeMD5(sourceID[4:])
	}
	return normalizeMD5(sourceID)
}

func normalizeMD5(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) != 32 {
		return ""
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return ""
		}
	}
	return s
}
