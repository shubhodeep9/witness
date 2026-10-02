package bunaudit

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/shubhodeep9/witness"
	"github.com/shubhodeep9/witness/store/memory"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

type User struct {
	bun.BaseModel `bun:"table:users"`

	ID       int64 `bun:",pk,autoincrement"`
	Name     string
	Password string
	Age      int
}

type Untracked struct {
	bun.BaseModel `bun:"table:untracked"`

	ID   int64 `bun:",pk,autoincrement"`
	Name string
}

func setup(t *testing.T, s witness.Store) (*bun.DB, *Hook) {
	t.Helper()
	sqldb, err := sql.Open("sqlite3", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	sqldb.SetMaxOpenConns(1) // each :memory: connection is its own database; also proves tx joining
	db := bun.NewDB(sqldb, sqlitedialect.New())
	var reg witness.Registry
	reg.Register(&User{}, witness.Options{Mask: []string{"Password"}, Exclude: []string{"ID"}})
	h := New(&reg, s)
	db.AddQueryHook(h)
	ctx := context.Background()
	for _, m := range []any{(*User)(nil), (*Untracked)(nil), (*LogEntry)(nil)} {
		if _, err := db.NewCreateTable().Model(m).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return db, h
}

func TestLifecycle(t *testing.T) {
	var mem memory.Store
	db, _ := setup(t, &mem)

	u := &User{Name: "a", Password: "pw", Age: 1}
	exec(t, db.NewInsert().Model(u))
	exec(t, db.NewUpdate().Model(&User{ID: u.ID, Name: "b", Password: "pw", Age: 1}).WherePK()) // WherePK, all columns
	exec(t, db.NewUpdate().Model((*User)(nil)).Set("age = ?", 2).Where("name = ?", "b"))        // bulk, by WHERE
	exec(t, db.NewUpdate().Model(&User{ID: u.ID, Name: "c"}).Column("name").WherePK())          // WherePK, one column
	exec(t, db.NewUpdate().Model((*User)(nil)).Set("age = ?", 2).Where("id = ?", u.ID))         // no-op: no entry
	exec(t, db.NewDelete().Model(u).WherePK())
	exec(t, db.NewInsert().Model(&Untracked{Name: "x"})) // unregistered: no entry

	got := mem.Entries()
	want := []struct {
		action witness.Action
		change string
		old    any
		new    any
	}{
		{witness.Create, "Name", nil, "a"},
		{witness.Update, "Name", "a", "b"},
		{witness.Update, "Age", int64(1), int64(2)},
		{witness.Update, "Name", "b", "c"},
		{witness.Delete, "Name", "c", nil},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		e := got[i]
		c := e.Changes[w.change]
		if e.Action != w.action || !same(c.Old, w.old) || !same(c.New, w.new) {
			t.Errorf("entry %d: %s %s %+v, want %s %v→%v", i, e.Action, w.change, c, w.action, w.old, w.new)
		}
		if e.ObjectType != "users" || e.ObjectID != "1" {
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

// numbers compare by value: sqlite may scan ints as int or int64
func same(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	if av.CanInt() && bv.CanInt() {
		return av.Int() == bv.Int()
	}
	return a == b
}

func TestActorFromContext(t *testing.T) {
	var mem memory.Store
	db, _ := setup(t, &mem)
	ctx := witness.WithMeta(context.Background(), witness.Meta{Actor: "u1", RemoteAddr: "1.2.3.4"})
	if _, err := db.NewInsert().Model(&User{Name: "a"}).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if e := mem.Entries()[0]; e.Actor != "u1" || e.RemoteAddr != "1.2.3.4" {
		t.Fatalf("got %+v", e)
	}
}

// With one pooled connection, reading via the root DB inside a tx would deadlock,
// and would miss the uncommitted row: this proves the hook joins the transaction.
func TestJoinsTransaction(t *testing.T) {
	var mem memory.Store
	db, _ := setup(t, &mem)
	ctx := context.Background()
	err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		u := &User{Name: "a"}
		if _, err := tx.NewInsert().Model(u).Exec(ctx); err != nil {
			return err
		}
		u.Name = "b"
		_, err := tx.NewUpdate().Model(u).WherePK().Exec(ctx)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got := mem.Entries()
	if len(got) != 2 || got[1].Changes["Name"].Old != "a" || got[1].Changes["Name"].New != "b" {
		t.Fatalf("got %+v", got)
	}
}

type failStore struct{}

func (failStore) Write(context.Context, witness.Entry) error { return errors.New("boom") }

func TestStoreFailureRollsBackTx(t *testing.T) {
	db, h := setup(t, failStore{})
	var reported []error
	h.OnError = func(err error) { reported = append(reported, err) }
	ctx := context.Background()

	err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&User{Name: "a"}).Exec(ctx)
		return err // the query itself "succeeds"; the failure surfaces at commit
	})
	if err == nil {
		t.Fatal("expected commit to fail")
	}
	if len(reported) != 1 || reported[0].Error() != "boom" {
		t.Fatalf("OnError got %v", reported)
	}
	n, _ := db.NewSelect().Model((*User)(nil)).Count(ctx)
	if n != 0 {
		t.Fatalf("row survived failed audit write: %d", n)
	}
}

// Pins the bun internals inspect relies on; rerun after upgrading bun.
func TestInspect(t *testing.T) {
	db, _ := setup(t, &memory.Store{})
	ctx := context.Background()

	s, err := inspect(db.NewUpdate().Model(&User{ID: 1}).Where("age = ?", 1).Where("name = ?", "a").WherePK())
	if err != nil || len(s.where) != 2 || !s.wherePK {
		t.Fatalf("update: %+v, %v", s, err)
	}
	if _, err := inspect(db.NewInsert().Model(&User{})); err != nil {
		t.Fatal(err)
	}
	if _, err := inspect(db.NewDelete().Model(&User{}).Where("id = 1")); err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if s, err := inspect(tx.NewInsert().Model(&User{})); err != nil {
		t.Fatal(err)
	} else if _, ok := s.conn.(*sql.Tx); !ok {
		t.Fatalf("conn is %T, want *sql.Tx", s.conn)
	}
}

func exec(t *testing.T, q interface {
	Exec(context.Context, ...any) (sql.Result, error)
}) {
	t.Helper()
	if _, err := q.Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
}
