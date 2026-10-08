package catalog

import (
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"
)

func TestCandidateFromAnnaKeepsEPUBAndNormalizesFields(t *testing.T) {
	r := AnnaRecord{
		ID: "md5:0123456789abcdef0123456789abcdef",
		Source: AnnaRecordSource{FileUnifiedData: AnnaFileUnifiedData{
			ExtensionBest: "EPUB",
			FilesizeBest: 12345,
			TitleBest: " Ondskan ",
			AuthorBest: "Jan Guillou",
			PublisherBest: "Publisher",
			YearBest: "1981",
			LanguageCodes: []string{"sv", "SV", "en"},
			IdentifiersUnified: map[string][]string{
				"isbn13": {"978-91-000000-0-0"},
				"md5": {"0123456789abcdef0123456789abcdef"},
			},
		}},
	}
	c, ok := CandidateFromAnna(r)
	if !ok { t.Fatal("expected epub candidate") }
	if c.Title != "Ondskan" || c.Author != "Jan Guillou" { t.Fatalf("unexpected candidate: %#v", c) }
	if c.MD5 != "0123456789abcdef0123456789abcdef" { t.Fatalf("md5=%q", c.MD5) }
	if len(c.Languages) != 2 || c.Languages[0] != "sv" || c.Languages[1] != "en" { t.Fatalf("languages=%v", c.Languages) }
}

func TestCandidateFromAnnaRejectsNonEPUB(t *testing.T) {
	_, ok := CandidateFromAnna(AnnaRecord{Source: AnnaRecordSource{FileUnifiedData: AnnaFileUnifiedData{ExtensionBest:"pdf",TitleBest:"Book"}}})
	if ok { t.Fatal("pdf should be ignored by initial ebook importer") }
}

func TestParseAnnaJSONLStreamsRecords(t *testing.T) {
	input := strings.Join([]string{
		`{"_id":"a","_source":{"file_unified_data":{"extension_best":"epub","title_best":"One"}}}`,
		`{"_id":"b","_source":{"file_unified_data":{"extension_best":"epub","title_best":"Two"}}}`,
	}, "\n")
	var ids []string
	seen, err := ParseAnnaJSONL(context.Background(), strings.NewReader(input), func(r AnnaRecord) error {
		ids = append(ids, r.ID)
		return nil
	})
	if err != nil { t.Fatal(err) }
	if seen != 2 || len(ids) != 2 || ids[0] != "a" || ids[1] != "b" { t.Fatalf("seen=%d ids=%v", seen, ids) }
}

func TestParseAnnaGzipJSONL(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write([]byte(`{"_id":"a","_source":{"file_unified_data":{"extension_best":"epub","title_best":"One"}}}` + "\n"))
	if err := gz.Close(); err != nil { t.Fatal(err) }
	seen, err := ParseAnnaGzipJSONL(context.Background(), &buf, func(r AnnaRecord) error { return nil })
	if err != nil { t.Fatal(err) }
	if seen != 1 { t.Fatalf("seen=%d", seen) }
}
