package catalog

import (
	"context"
	"database/sql"
	"testing"
	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) (*sql.DB,*Store) {
	db,err:=sql.Open("sqlite",":memory:")
	if err!=nil { t.Fatal(err) }
	db.Exec(`PRAGMA foreign_keys=ON`)
	if err:=Migrate(context.Background(),db);err!=nil { t.Fatal(err) }
	return db,NewStore(db)
}

func TestBestReleasePrefersSwedishThenEnglish(t *testing.T) {
	db,s:=testStore(t); defer db.Close()
	res,_:=db.Exec(`INSERT INTO catalog_books(canonical_key,title,author) VALUES('work:1','Ondskan','Jan Guillou')`)
	id,_:=res.LastInsertId()
	db.Exec(`INSERT INTO catalog_releases(book_id,source,source_id,format,language,size_bytes) VALUES
		(?, 'annas','en1','epub','en',100),(?, 'annas','sv1','epub','sv',50),(?, 'annas','da1','epub','da',200)`,id,id,id)
	r,err:=s.BestRelease(context.Background(),id,"")
	if err!=nil { t.Fatal(err) }
	if r==nil || r.Language!="sv" { t.Fatalf("wanted sv, got %#v",r) }
}

func TestRequestedLanguageOverridesPreference(t *testing.T) {
	db,s:=testStore(t); defer db.Close()
	res,_:=db.Exec(`INSERT INTO catalog_books(canonical_key,title) VALUES('work:1','Book')`); id,_:=res.LastInsertId()
	db.Exec(`INSERT INTO catalog_releases(book_id,source,source_id,format,language) VALUES(?, 'annas','sv','epub','sv'),(?, 'annas','es','epub','es')`,id,id)
	r,err:=s.BestRelease(context.Background(),id,"es")
	if err!=nil { t.Fatal(err) }
	if r==nil || r.Language!="es" { t.Fatalf("wanted es, got %#v",r) }
}

func TestReactionsAreIndependent(t *testing.T) {
	db,s:=testStore(t); defer db.Close()
	res,_:=db.Exec(`INSERT INTO catalog_books(canonical_key,title) VALUES('work:1','Book')`); book,_:=res.LastInsertId()
	db.Exec(`INSERT INTO catalog_profiles(name) VALUES('Linda'),('Other')`)
	if err:=s.SetReaction(context.Background(),1,book,1);err!=nil { t.Fatal(err) }
	if err:=s.SetReaction(context.Background(),2,book,-1);err!=nil { t.Fatal(err) }
	var a,b int
	db.QueryRow(`SELECT reaction FROM catalog_profile_reactions WHERE profile_id=1 AND book_id=?`,book).Scan(&a)
	db.QueryRow(`SELECT reaction FROM catalog_profile_reactions WHERE profile_id=2 AND book_id=?`,book).Scan(&b)
	if a!=1 || b!=-1 { t.Fatalf("unexpected reactions %d %d",a,b) }
}
