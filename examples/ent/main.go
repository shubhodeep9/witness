// Example: audit an Ent entity. From the examples directory:
//
//	(cd ent/ent && go generate ./...)   # once: generates the Ent client
//	go run ./ent
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/shubhodeep9/witness"
	"github.com/shubhodeep9/witness/entaudit"
	"github.com/shubhodeep9/witness/examples/ent/ent"
	"github.com/shubhodeep9/witness/store/memory"
)

func main() {
	drv, err := entsql.Open("sqlite3", "file::memory:?_fk=1")
	if err != nil {
		log.Fatal(err)
	}
	drv.DB().SetMaxOpenConns(1) // each :memory: connection is its own database
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()

	ctx := context.Background()
	if err := client.Schema.Create(ctx); err != nil {
		log.Fatal(err)
	}

	// Opt the entity in: ignore UpdatedAt, mask Password.
	var reg witness.Registry
	reg.Register(&ent.User{}, witness.Options{
		Exclude: []string{"UpdatedAt"},
		Mask:    []string{"Password"},
	})

	var store memory.Store // swap for your own witness.Store to persist entries
	client.Use(entaudit.Hook(&reg, &store))

	// Who did it: normally set per request (e.g. from auth middleware).
	ctx = witness.WithMeta(ctx, witness.Meta{Actor: "alice", RemoteAddr: "203.0.113.7"})

	u, err := client.User.Create().SetName("Bob").SetPassword("hunter2").Save(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := client.User.UpdateOneID(u.ID).SetName("Robert").Save(ctx); err != nil {
		log.Fatal(err)
	}
	if err := client.User.DeleteOneID(u.ID).Exec(ctx); err != nil {
		log.Fatal(err)
	}

	for _, e := range store.Entries() {
		b, _ := json.Marshal(e.Changes)
		fmt.Printf("%-6s %s#%s by %s  %s\n", e.Action, e.ObjectType, e.ObjectID, e.Actor, b)
	}
}
