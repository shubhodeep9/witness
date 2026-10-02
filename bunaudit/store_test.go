package bunaudit

import (
	"context"
	"errors"
	"testing"

	"github.com/shubhodeep9/witness"
	"github.com/uptrace/bun"
)

func TestDBStore(t *testing.T) {
	db, _ := setup(t, NewStore())
	ctx := witness.WithMeta(context.Background(), witness.Meta{Actor: "u1", RemoteAddr: "1.2.3.4"})

	u := &User{Name: "a", Password: "pw"}
	exec(t, db.NewInsert().Model(u))
	u.Name = "b"
	if _, err := db.NewUpdate().Model(u).WherePK().Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewDelete().Model(u).WherePK().Exec(ctx); err != nil {
		t.Fatal(err)
	}

	var rows []LogEntry
	if err := db.NewSelect().Model(&rows).Order("id").Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows: %+v", len(rows), rows)
	}
	for i, a := range []witness.Action{witness.Create, witness.Update, witness.Delete} {
		e := rows[i].Entry()
		if e.Action != a || e.ObjectType != "users" || e.ObjectID != "1" || e.Timestamp.IsZero() {
			t.Errorf("row %d: %+v", i, e)
		}
	}
	if e := rows[1].Entry(); e.Actor != "u1" || e.RemoteAddr != "1.2.3.4" {
		t.Errorf("meta lost: %+v", e)
	}
	if c := rows[1].Entry().Changes["Name"]; c.Old != "a" || c.New != "b" {
		t.Errorf("update changes: %+v", rows[1].Changes)
	}
	if c := rows[0].Entry().Changes["Password"]; c.New != "****" {
		t.Errorf("password not masked: %+v", c)
	}
}

// the entry lives in the audited write's transaction, so a rollback removes both
func TestDBStoreRollsBackWithChange(t *testing.T) {
	db, _ := setup(t, NewStore())
	ctx := context.Background()
	err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(&User{Name: "a"}).Exec(ctx); err != nil {
			return err
		}
		return errors.New("abort")
	})
	if err == nil {
		t.Fatal("expected abort")
	}
	users, _ := db.NewSelect().Model((*User)(nil)).Count(ctx)
	logs, _ := db.NewSelect().Model((*LogEntry)(nil)).Count(ctx)
	if users != 0 || logs != 0 {
		t.Fatalf("survived rollback: %d users, %d log rows", users, logs)
	}
}

func TestDBStoreOutsideHook(t *testing.T) {
	if err := NewStore().Write(context.Background(), witness.Entry{}); err == nil {
		t.Fatal("expected error")
	}
}
