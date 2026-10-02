// Example: audit a GORM model. Run with `go run ./examples/gorm`.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/shubhodeep9/witness"
	"github.com/shubhodeep9/witness/gormaudit"
	"github.com/shubhodeep9/witness/store/memory"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type User struct {
	ID        uint
	Name      string
	Password  string
	UpdatedAt int64
}

func main() {
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	if err := db.AutoMigrate(&User{}); err != nil {
		log.Fatal(err)
	}

	// Opt the model in: ignore UpdatedAt, mask Password.
	var reg witness.Registry
	reg.Register(&User{}, witness.Options{
		Exclude: []string{"UpdatedAt"},
		Mask:    []string{"Password"},
	})

	var store memory.Store // swap for your own witness.Store to persist entries
	if err := db.Use(gormaudit.New(&reg, &store)); err != nil {
		log.Fatal(err)
	}

	// Who did it: normally set per request (e.g. from auth middleware).
	ctx := witness.WithMeta(context.Background(), witness.Meta{Actor: "alice", RemoteAddr: "203.0.113.7"})
	db = db.WithContext(ctx)

	u := User{Name: "Bob", Password: "hunter2"}
	db.Create(&u)
	db.Model(&u).Update("name", "Robert")
	db.Delete(&u)

	for _, e := range store.Entries() {
		b, _ := json.Marshal(e.Changes)
		fmt.Printf("%-6s %s#%s by %s  %s\n", e.Action, e.ObjectType, e.ObjectID, e.Actor, b)
	}
}
