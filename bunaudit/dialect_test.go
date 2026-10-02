package bunaudit

import (
	"database/sql"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

// open returns a fresh database: SQLite in memory, or, when WITNESS_TEST_PG holds a Postgres
// DSN, a private schema in that server (dropped when the test ends).
func open(t *testing.T) *bun.DB {
	t.Helper()
	dsn := os.Getenv("WITNESS_TEST_PG")
	if dsn == "" {
		sqldb, err := sql.Open("sqlite3", "file::memory:")
		if err != nil {
			t.Fatal(err)
		}
		sqldb.SetMaxOpenConns(1) // each :memory: connection is its own database; also proves tx joining
		return bun.NewDB(sqldb, sqlitedialect.New())
	}

	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
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
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn + sep + "search_path=" + schema)))
	sqldb.SetMaxOpenConns(1) // a read outside an open tx would deadlock: proves tx joining
	t.Cleanup(func() { sqldb.Close() })
	return bun.NewDB(sqldb, pgdialect.New())
}
