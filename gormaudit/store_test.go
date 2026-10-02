package gormaudit

import (
	"context"
	"errors"
	"testing"

	"github.com/shubhodeep9/witness"
	"gorm.io/gorm"
)

func TestDBStore(t *testing.T) {
	db := setup(t, NewStore())
	ctx := witness.WithMeta(context.Background(), witness.Meta{Actor: "u1", RemoteAddr: "1.2.3.4"})
	db = db.WithContext(ctx)

	u := User{Name: "a", Password: "pw"}
	must(t, db.Create(&u).Error)
	must(t, db.Model(&u).Update("name", "b").Error)
	must(t, db.Delete(&u).Error)

	var rows []LogEntry
	must(t, db.Order("id").Find(&rows).Error)
	if len(rows) != 3 {
		t.Fatalf("got %d rows: %+v", len(rows), rows)
	}
	for i, a := range []witness.Action{witness.Create, witness.Update, witness.Delete} {
		e := rows[i].Entry()
		if e.Action != a || e.ObjectType != "users" || e.ObjectID != "1" || e.Actor != "u1" || e.RemoteAddr != "1.2.3.4" || e.Timestamp.IsZero() {
			t.Errorf("row %d: %+v", i, e)
		}
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
	db := setup(t, NewStore())
	err := db.Transaction(func(tx *gorm.DB) error {
		must(t, tx.Create(&User{Name: "a"}).Error)
		return errors.New("abort")
	})
	if err == nil {
		t.Fatal("expected abort")
	}
	var users, logs int64
	db.Model(&User{}).Count(&users)
	db.Model(&LogEntry{}).Count(&logs)
	if users != 0 || logs != 0 {
		t.Fatalf("survived rollback: %d users, %d log rows", users, logs)
	}
}

func TestDBStoreOutsidePlugin(t *testing.T) {
	if err := NewStore().Write(context.Background(), witness.Entry{}); err == nil {
		t.Fatal("expected error")
	}
}
