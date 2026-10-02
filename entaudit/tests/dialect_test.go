package tests

import (
	"database/sql"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"
)

// open returns a fresh database driver: SQLite in memory, or, when WITNESS_TEST_PG holds a
// Postgres DSN, a private schema in that server (dropped when the test ends).
func open(t *testing.T) *entsql.Driver {
	t.Helper()
	dsn := os.Getenv("WITNESS_TEST_PG")
	if dsn == "" {
		drv, err := entsql.Open("sqlite3", "file:"+strings.NewReplacer("/", "_").Replace(t.Name())+"?mode=memory&cache=shared&_fk=1")
		if err != nil {
			t.Fatal(err)
		}
		drv.DB().SetMaxOpenConns(1) // a read outside an open tx would deadlock: proves tx joining
		return drv
	}

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	h := fnv.New32a()
	h.Write([]byte(t.Name()))
	schema := fmt.Sprintf("t_%x", h.Sum32())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		admin.Close()
	})

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, err := sql.Open("pgx", dsn+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1) // a read outside an open tx would deadlock: proves tx joining
	t.Cleanup(func() { db.Close() })
	return entsql.OpenDB(dialect.Postgres, db) // ent doesn't know the "pgx" driver name
}
