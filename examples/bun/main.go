// Example: audit a Bun model. Run with `go run ./bun` from the examples directory.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
	"github.com/shubhodeep9/witness"
	"github.com/shubhodeep9/witness/bunaudit"
	"github.com/shubhodeep9/witness/store/memory"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

type User struct {
	bun.BaseModel `bun:"table:users"`

	ID        int64 `bun:",pk,autoincrement"`
	Name      string
	Password  string
	UpdatedAt int64
}

func main() {
	sqldb, err := sql.Open("sqlite3", "file::memory:")
	if err != nil {
		log.Fatal(err)
	}
	sqldb.SetMaxOpenConns(1) // each :memory: connection is its own database
	db := bun.NewDB(sqldb, sqlitedialect.New())

	ctx := context.Background()
	if _, err := db.NewCreateTable().Model((*User)(nil)).Exec(ctx); err != nil {
		log.Fatal(err)
	}

	// Opt the model in: ignore UpdatedAt, mask Password.
	var reg witness.Registry
	reg.Register(&User{}, witness.Options{
		Exclude: []string{"UpdatedAt"},
		Mask:    []string{"Password"},
	})

	var store memory.Store // swap for your own witness.Store to persist entries
	db.AddQueryHook(bunaudit.New(&reg, &store))

	// Who did it: normally set per request (e.g. from auth middleware).
	ctx = witness.WithMeta(ctx, witness.Meta{Actor: "alice", RemoteAddr: "203.0.113.7"})

	u := &User{Name: "Bob", Password: "hunter2"}
	if _, err := db.NewInsert().Model(u).Exec(ctx); err != nil {
		log.Fatal(err)
	}
	u.Name = "Robert"
	if _, err := db.NewUpdate().Model(u).WherePK().Exec(ctx); err != nil {
		log.Fatal(err)
	}
	if _, err := db.NewDelete().Model(u).WherePK().Exec(ctx); err != nil {
		log.Fatal(err)
	}

	for _, e := range store.Entries() {
		b, _ := json.Marshal(e.Changes)
		fmt.Printf("%-6s %s#%s by %s  %s\n", e.Action, e.ObjectType, e.ObjectID, e.Actor, b)
	}
}
