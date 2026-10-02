package gormaudit

import (
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// open returns a fresh database: SQLite in memory, or, when WITNESS_TEST_PG holds a Postgres
// DSN, a private schema in that server (dropped when the test ends).
func open(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("WITNESS_TEST_PG")
	if dsn == "" {
		db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, _ := db.DB()
		sqlDB.SetMaxOpenConns(1) // each :memory: connection is its own database
		return db
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	h := fnv.New32a()
	h.Write([]byte(t.Name()))
	schema := fmt.Sprintf("t_%x", h.Sum32())
	must(t, admin.Exec("CREATE SCHEMA "+schema).Error)
	t.Cleanup(func() {
		admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		sqlDB, _ := admin.DB()
		sqlDB.Close()
	})

	sep := " "
	if strings.Contains(dsn, "://") {
		sep = "&"
		if !strings.Contains(dsn, "?") {
			sep = "?"
		}
		dsn += sep + "search_path=" + schema
	} else {
		dsn += sep + "search_path=" + schema
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1) // a read outside an open tx would deadlock: proves tx joining
	t.Cleanup(func() { sqlDB.Close() })
	return db
}
