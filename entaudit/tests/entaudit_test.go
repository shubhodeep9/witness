// Package tests exercises entaudit against generated Ent code. It is a separate module so
// the published entaudit module carries no tests that import ungenerated packages.
package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/shubhodeep9/witness"
	"github.com/shubhodeep9/witness/entaudit"
	"github.com/shubhodeep9/witness/entaudit/tests/internal/testent"
	"github.com/shubhodeep9/witness/entaudit/tests/internal/testent/user"
	"github.com/shubhodeep9/witness/store/memory"
)

func setup(t *testing.T, s witness.Store) *testent.Client {
	t.Helper()
	drv := open(t)
	client := testent.NewClient(testent.Driver(drv))
	t.Cleanup(func() { client.Close() })
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
	var reg witness.Registry
	reg.Register(&testent.User{}, witness.Options{Mask: []string{"Password"}, Exclude: []string{"ID"}})
	client.Use(entaudit.Hook(&reg, s))
	return client
}

func TestLifecycle(t *testing.T) {
	var mem memory.Store
	client := setup(t, &mem)
	ctx := witness.WithMeta(context.Background(), witness.Meta{Actor: "u1", RemoteAddr: "1.2.3.4"})

	u := client.User.Create().SetName("a").SetPassword("pw").SetAge(1).SaveX(ctx)
	client.User.UpdateOneID(u.ID).SetName("b").SaveX(ctx)             // update one
	client.User.Update().Where(user.NameEQ("b")).AddAge(1).ExecX(ctx) // bulk, with Add
	client.User.UpdateOneID(u.ID).SetName("b").SaveX(ctx)             // no-op: no entry
	client.User.DeleteOneID(u.ID).ExecX(ctx)                          // delete one
	client.Note.Create().SetTitle("x").SaveX(ctx)                     // unregistered: no entry
	client.User.Create().SetName("c").SaveX(ctx)                      // second row for bulk delete
	client.User.Delete().Where(user.NameEQ("c")).ExecX(ctx)           // bulk delete

	got := mem.Entries()
	want := []struct {
		action witness.Action
		change string
		old    any
		new    any
	}{
		{witness.Create, "Name", nil, "a"},
		{witness.Update, "Name", "a", "b"},
		{witness.Update, "Age", 1, 2},
		{witness.Delete, "Name", "b", nil},
		{witness.Create, "Name", nil, "c"},
		{witness.Delete, "Name", "c", nil},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		e := got[i]
		c := e.Changes[w.change]
		if e.Action != w.action || c.Old != w.old || c.New != w.new {
			t.Errorf("entry %d: %s %s %+v, want %s %v→%v", i, e.Action, w.change, c, w.action, w.old, w.new)
		}
		if e.ObjectType != "User" || e.Actor != "u1" || e.RemoteAddr != "1.2.3.4" {
			t.Errorf("entry %d metadata: %+v", i, e)
		}
		if _, ok := e.Changes["ID"]; ok {
			t.Errorf("entry %d: excluded field ID present", i)
		}
	}
	if pw := got[0].Changes["Password"]; pw.New != "****" {
		t.Errorf("password not masked: %+v", pw)
	}
}

func TestJoinsTransaction(t *testing.T) {
	var mem memory.Store
	client := setup(t, &mem)
	ctx := context.Background()

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u := tx.User.Create().SetName("a").SaveX(ctx)
	tx.User.UpdateOneID(u.ID).SetName("b").SaveX(ctx) // pre-read must see the uncommitted row
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got := mem.Entries()
	if len(got) != 2 || got[1].Changes["Name"].Old != "a" || got[1].Changes["Name"].New != "b" {
		t.Fatalf("got %+v", got)
	}
}

type failStore struct{}

func (failStore) Write(context.Context, witness.Entry) error { return errors.New("boom") }

func TestStoreFailureFailsOperation(t *testing.T) {
	client := setup(t, failStore{})
	ctx := context.Background()

	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.User.Create().SetName("a").Save(ctx); err == nil || err.Error() != "boom" {
		t.Fatalf("expected store error, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if n := client.User.Query().CountX(ctx); n != 0 {
		t.Fatalf("row survived rollback: %d", n)
	}
}

func TestMutationInContext(t *testing.T) {
	var seen bool
	client := setup(t, storeFunc(func(ctx context.Context, e witness.Entry) error {
		_, ok := entaudit.Mutation(ctx).(interface{ Client() *testent.Client })
		seen = ok
		return nil
	}))
	client.User.Create().SetName("a").SaveX(context.Background())
	if !seen {
		t.Fatal("Mutation(ctx) not usable from a store")
	}
}

type storeFunc func(context.Context, witness.Entry) error

func (f storeFunc) Write(ctx context.Context, e witness.Entry) error { return f(ctx, e) }
