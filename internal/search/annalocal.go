package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/JeremiahM37/librarr/internal/config"
	"github.com/JeremiahM37/librarr/internal/models"
)

// AnnaLocalAPI uses the local PostgreSQL-backed anna-api service for searches.
// It deliberately keeps the public source name "annas" so the existing Anna
// membership download path can consume the returned MD5 without changes.
type AnnaLocalAPI struct {
	cfg     *config.Config
	client  *http.Client
	baseURL string
}

type annaLocalSearchResponse struct {
	Total   int64             `json:"total"`
	Results []annaLocalRecord `json:"results"`
}

type annaLocalRecord struct {
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	Publisher       string              `json:"publisher"`
	Author          string              `json:"author"`
	CoverURL        string              `json:"coverURL"`
	Year            int                 `json:"year"`
	Languages       []string            `json:"languages"`
	Description     string              `json:"description"`
	Identifiers     []annaLocalKeyValue `json:"identifiers"`
	Classifications []annaLocalKeyValue `json:"classifications"`
}

type annaLocalKeyValue struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

var isbnLikeRe = regexp.MustCompile(`^[0-9Xx -]{10,20}$`)

func NewAnnaLocalAPI(cfg *config.Config, client *http.Client) *AnnaLocalAPI {
	return newAnnaLocalAPI(cfg, client, os.Getenv("ANNA_LOCAL_API_URL"))
}

func newAnnaLocalAPI(cfg *config.Config, client *http.Client, rawBaseURL string) *AnnaLocalAPI {
	base := strings.TrimSpace(rawBaseURL)
	if base != "" && !strings.Contains(base, "://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")
	return &AnnaLocalAPI{cfg: cfg, client: client, baseURL: base}
}

func (a *AnnaLocalAPI) Name() string         { return "annas" }
func (a *AnnaLocalAPI) Label() string        { return "Anna's Archive (local index)" }
func (a *AnnaLocalAPI) Enabled() bool        { return a.baseURL != "" }
func (a *AnnaLocalAPI) SearchTab() string    { return "main" }
func (a *AnnaLocalAPI) DownloadType() string { return "direct" }

func (a *AnnaLocalAPI) Search(ctx context.Context, query string) ([]models.SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []models.SearchResult{}, nil
	}
	if a.client == nil {
		return nil, fmt.Errorf("anna local api: HTTP client is nil")
	}

	byID := make(map[string]annaLocalRecord)

	// ISBN lookup is exact and avoids text-search ambiguity when the user pastes
	// an ISBN. The text calls below are still useful for normal title/author input.
	if isbn := compactISBN(query); isbn != "" {
		records, err := a.searchISBN(ctx, isbn)
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			byID[r.ID] = r
		}
	}

	// anna-api's text endpoint combines populated fields with AND logic. For a
	// free-form client query we therefore search title OR author as two local
	// requests and merge by record ID.
	for _, fields := range [][2]string{{query, ""}, {"", query}} {
		records, err := a.searchText(ctx, fields[0], fields[1])
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			if r.ID != "" {
				byID[r.ID] = r
			}
		}
	}

	results := make([]models.SearchResult, 0, len(byID))
	for _, r := range byID {
		results = append(results, mapAnnaLocalRecord(r))
	}

	// Make language preference deterministic before the normal Librarr scoring
	// layer runs. We keep all languages; this is ranking, not filtering.
	sort.SliceStable(results, func(i, j int) bool {
		li := languageRank(results[i].Language)
		lj := languageRank(results[j].Language)
		if li != lj {
			return li < lj
		}
		if results[i].Title != results[j].Title {
			return strings.ToLower(results[i].Title) < strings.ToLower(results[j].Title)
		}
		return results[i].SourceID < results[j].SourceID
	})

	if len(results) > 100 {
		results = results[:100]
	}
	return results, nil
}

func (a *AnnaLocalAPI) searchText(ctx context.Context, title, author string) ([]annaLocalRecord, error) {
	v := url.Values{}
	// Both parameters are sent even when empty because anna-api marks them as
	// required in its request schema while its DB layer intentionally ignores
	// empty values.
	v.Set("title", title)
	v.Set("author", author)
	v.Set("limit", "50")
	v.Set("offset", "0")
	return a.doSearch(ctx, "/v1/search/text?"+v.Encode())
}

func (a *AnnaLocalAPI) searchISBN(ctx context.Context, isbn string) ([]annaLocalRecord, error) {
	v := url.Values{}
	v.Set("isbn", isbn)
	v.Set("limit", "50")
	v.Set("offset", "0")
	return a.doSearch(ctx, "/v1/search/isbn?"+v.Encode())
}

func (a *AnnaLocalAPI) doSearch(ctx context.Context, path string) ([]annaLocalRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if a.cfg != nil && a.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", a.cfg.UserAgent)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anna local api request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anna local api: HTTP %d", resp.StatusCode)
	}
	var body annaLocalSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("anna local api decode: %w", err)
	}
	return body.Results, nil
}

func mapAnnaLocalRecord(r annaLocalRecord) models.SearchResult {
	md5 := annaLocalMD5(r)
	return models.SearchResult{
		Source:      "annas",
		Title:       strings.TrimSpace(r.Title),
		Author:      strings.TrimSpace(r.Author),
		SourceID:    r.ID,
		MD5:         md5,
		CoverURL:    strings.TrimSpace(r.CoverURL),
		Format:      "epub",
		MediaType:   "ebook",
		Language:    bestAnnaLanguage(r.Languages),
		Publisher:   strings.TrimSpace(r.Publisher),
		Year:        yearString(r.Year),
		SizeHuman:   "",
		DownloadURL: "",
	}
}

func annaLocalMD5(r annaLocalRecord) string {
	for _, id := range r.Identifiers {
		if strings.EqualFold(strings.TrimSpace(id.Type), "md5") {
			if md5 := normalizeAnnaMD5(id.Value); md5 != "" {
				return md5
			}
		}
	}
	id := strings.TrimSpace(r.ID)
	if strings.HasPrefix(strings.ToLower(id), "md5:") {
		id = id[4:]
	}
	return normalizeAnnaMD5(id)
}

func normalizeAnnaMD5(s string) string {
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

func compactISBN(q string) string {
	if !isbnLikeRe.MatchString(strings.TrimSpace(q)) {
		return ""
	}
	q = strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(q))
	if len(q) != 10 && len(q) != 13 {
		return ""
	}
	return strings.ToUpper(q)
}

func bestAnnaLanguage(langs []string) string {
	best := ""
	bestRank := 999
	for _, raw := range langs {
		lang := strings.ToLower(strings.TrimSpace(raw))
		if lang == "" {
			continue
		}
		rank := languageRank(lang)
		if best == "" || rank < bestRank {
			best, bestRank = lang, rank
		}
	}
	return best
}

func languageRank(lang string) int {
	lang = strings.ToLower(strings.TrimSpace(lang))
	base := strings.SplitN(lang, "-", 2)[0]
	switch base {
	case "sv":
		return 0
	case "en":
		return 1
	case "da":
		return 2
	case "es":
		return 3
	default:
		return 100
	}
}

func yearString(year int) string {
	if year <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", year)
}
