package gormaudit

import (
	"context"
	"errors"
	"testing"

	"github.com/shubhodeep9/witness"
	"github.com/shubhodeep9/witness/store/memory"
	"gorm.io/gorm"
)

type User struct {
	ID       uint
	Name     string
	Password string
	Age      int
}

type Untracked struct {
	ID   uint
	Name string
}

func setup(t *testing.T, s witness.Store) *gorm.DB {
	t.Helper()
	db := open(t)
	var reg witness.Registry
	reg.Register(&User{}, witness.Options{Mask: []string{"Password"}, Exclude: []string{"ID"}})
	if err := db.Use(New(&reg, s)); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&User{}, &Untracked{}, &LogEntry{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLifecycle(t *testing.T) {
	var mem memory.Store
	db := setup(t, &mem)
	ctx := witness.WithMeta(context.Background(), witness.Meta{Actor: "u1", RemoteAddr: "1.2.3.4"})
	db = db.WithContext(ctx)

	u := User{Name: "a", Password: "pw", Age: 1}
	must(t, db.Create(&u).Error)
	must(t, db.Save(&User{ID: u.ID, Name: "b", Password: "pw", Age: 1}).Error)    // Save
	must(t, db.Model(&User{}).Where("name = ?", "b").Update("age", 2).Error)      // bulk, by WHERE
	must(t, db.Model(&User{ID: u.ID}).Updates(map[string]any{"name": "c"}).Error) // map, by model pk
	must(t, db.Model(&User{}).Where("id = ?", u.ID).Update("age", 2).Error)       // no-op: no entry
	must(t, db.Delete(&u).Error)
	must(t, db.Create(&Untracked{Name: "x"}).Error) // unregistered: no entry

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
		{witness.Update, "Name", "b", "c"},
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
		if e.ObjectType != "users" || e.ObjectID != "1" || e.Actor != "u1" || e.RemoteAddr != "1.2.3.4" {
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

type failStore struct{ fail bool }

func (f *failStore) Write(context.Context, witness.Entry) error {
	if f.fail {
		return errors.New("boom")
	}
	return nil
}

// a failing audit write must roll back the audited change itself
func TestStoreFailureRollsBack(t *testing.T) {
	fs := &failStore{}
	db := setup(t, fs)
	u := User{Name: "a"}
	must(t, db.Create(&u).Error)

	fs.fail = true
	if db.Model(&u).Update("name", "b").Error == nil {
		t.Fatal("update: expected store error")
	}
	if db.Delete(&u).Error == nil {
		t.Fatal("delete: expected store error")
	}
	if db.Create(&User{Name: "c"}).Error == nil {
		t.Fatal("create: expected store error")
	}

	var users []User
	must(t, db.Find(&users).Error)
	if len(users) != 1 || users[0].Name != "a" {
		t.Fatalf("changes survived failed audit writes: %+v", users)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
