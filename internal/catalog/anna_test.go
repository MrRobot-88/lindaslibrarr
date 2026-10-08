package catalog

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type rewriteTransport struct{ target string }
func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response,error) {
	clone:=req.Clone(req.Context())
	clone.URL.Scheme="http"
	clone.URL.Host=strings.TrimPrefix(r.target,"http://")
	return http.DefaultTransport.RoundTrip(clone)
}

func TestLatestMetadataTorrentSelectsNewestActive(t *testing.T) {
	ts:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.URL.Path!="/dyn/torrents.json" { t.Fatalf("path=%s",r.URL.Path) }
		w.Header().Set("Content-Type","application/json")
		_,_=w.Write([]byte(`[
		{"display_name":"old","group_name":"aa_derived_mirror_metadata","top_level_group_name":"other_aa","obsolete":false,"added_to_torrents_list_at":"2026-01-01","magnet_link":"magnet:?xt=old"},
		{"display_name":"new","group_name":"aa_derived_mirror_metadata","top_level_group_name":"other_aa","obsolete":false,"added_to_torrents_list_at":"2026-10-01","magnet_link":"magnet:?xt=new"},
		{"display_name":"obsolete","group_name":"aa_derived_mirror_metadata","top_level_group_name":"other_aa","obsolete":true,"added_to_torrents_list_at":"2026-12-01"}
		]`))
	}))
	defer ts.Close()
	client:=&http.Client{Transport:rewriteTransport{target:ts.URL},Timeout:time.Second}
	a:=AnnaCatalog{Domain:"annas-archive.gd",Client:client}
	got,err:=a.LatestMetadataTorrent(context.Background())
	if err!=nil { t.Fatal(err) }
	if got.DisplayName!="new" { t.Fatalf("wanted new, got %s",got.DisplayName) }
}

func TestAnnaSyncDue(t *testing.T) {
	db,err:=sql.Open("sqlite",":memory:"); if err!=nil {t.Fatal(err)}; defer db.Close()
	if err:=Migrate(context.Background(),db);err!=nil {t.Fatal(err)}
	s:=NewStore(db)
	due,err:=s.AnnaSyncDue(context.Background(),"torrent-A",24*time.Hour)
	if err!=nil || !due { t.Fatalf("first sync should be due: due=%v err=%v",due,err) }
	if err:=s.MarkAnnaSyncCompleted(context.Background(),"torrent-A",100,90);err!=nil {t.Fatal(err)}
	due,err=s.AnnaSyncDue(context.Background(),"torrent-A",24*time.Hour)
	if err!=nil || due { t.Fatalf("fresh same base should not be due: due=%v err=%v",due,err) }
	due,err=s.AnnaSyncDue(context.Background(),"torrent-B",24*time.Hour)
	if err!=nil || !due { t.Fatalf("new base should be due: due=%v err=%v",due,err) }
}
