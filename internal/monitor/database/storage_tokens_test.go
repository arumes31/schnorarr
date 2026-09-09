package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestEngineTokenPersistsAndIsDistinct(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	a, err := engineStorageToken(db, "1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := engineStorageToken(db, "2")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 || len(b) != 64 || a == b {
		t.Fatal("tokens are not distinct random identities")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := engineStorageToken(reopened, "1")
	if err != nil {
		t.Fatal(err)
	}
	if after != a {
		t.Fatal("engine token changed on restart")
	}
}
