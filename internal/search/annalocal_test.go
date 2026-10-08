package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JeremiahM37/librarr/internal/config"
)

func TestAnnaLocalSearchMergesTitleAndAuthorAndMapsMD5(t *testing.T) {
	var calls []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Query().Get("title") == "Jan Guillou":
			_, _ = w.Write([]byte(`{"total":1,"results":[{"id":"md5:0123456789abcdef0123456789abcdef","title":"Ondskan","author":"Jan Guillou","publisher":"Norstedts","coverURL":"https://covers.example/ondskan.jpg","year":1981,"languages":["en","sv"],"identifiers":[{"type":"md5","value":"0123456789abcdef0123456789abcdef"}]}]}`))
		case r.URL.Query().Get("author") == "Jan Guillou":
			_, _ = w.Write([]byte(`{"total":2,"results":[{"id":"md5:0123456789abcdef0123456789abcdef","title":"Ondskan","author":"Jan Guillou","year":1981,"languages":["sv"]},{"id":"md5:fedcba9876543210fedcba9876543210","title":"Brobyggarna","author":"Jan Guillou","year":2011,"languages":["en"]}]}`))
		default:
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
	}))
	defer ts.Close()

	a := newAnnaLocalAPI(&config.Config{UserAgent:"test-agent"}, ts.Client(), ts.URL)
	got, err := a.Search(context.Background(), "Jan Guillou")
	if err != nil { t.Fatal(err) }
	if len(calls) != 2 { t.Fatalf("calls=%d, want 2", len(calls)) }
	if len(got) != 2 { t.Fatalf("results=%d, want 2: %#v", len(got), got) }

	var ondskanFound bool
	for _, r := range got {
		if r.Title == "Ondskan" {
			ondskanFound = true
			if r.MD5 != "0123456789abcdef0123456789abcdef" { t.Fatalf("md5=%q", r.MD5) }
			if r.Language != "sv" { t.Fatalf("language=%q, want sv", r.Language) }
			if r.Format != "epub" || r.MediaType != "ebook" { t.Fatalf("format/media=%q/%q", r.Format, r.MediaType) }
			if r.Source != "annas" { t.Fatalf("source=%q", r.Source) }
		}
	}
	if !ondskanFound { t.Fatal("Ondskan missing") }
}

func TestAnnaLocalSearchISBNUsesExactEndpoint(t *testing.T) {
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":0,"results":[]}`))
	}))
	defer ts.Close()

	a := newAnnaLocalAPI(&config.Config{}, ts.Client(), ts.URL)
	if _, err := a.Search(context.Background(), "978-91-000000-0-0"); err != nil { t.Fatal(err) }
	if len(paths) != 3 { t.Fatalf("paths=%v, want ISBN + title + author", paths) }
	if !strings.HasPrefix(paths[0], "/v1/search/isbn?") { t.Fatalf("first path=%q", paths[0]) }
}

func TestAnnaLocalSearchReturnsHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer ts.Close()
	a := newAnnaLocalAPI(&config.Config{}, ts.Client(), ts.URL)
	_, err := a.Search(context.Background(), "Ondskan")
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") { t.Fatalf("err=%v", err) }
}

func TestBestAnnaLanguagePreference(t *testing.T) {
	if got := bestAnnaLanguage([]string{"es", "en", "da", "sv"}); got != "sv" { t.Fatalf("got %q", got) }
	if got := bestAnnaLanguage([]string{"fr", "de"}); got != "fr" { t.Fatalf("got %q", got) }
}
