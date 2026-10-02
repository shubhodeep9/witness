package audit

import (
	"context"
	"reflect"
	"testing"

	"github.com/shubhodeep9/witness"
	"github.com/shubhodeep9/witness/store/memory"
)

type row struct{ N int }

func (r *row) String() string { return "row!" }

func TestDeref(t *testing.T) {
	s, np := "x", (*string)(nil)
	ps := &s
	for _, c := range []struct{ in, want any }{{1, 1}, {&s, "x"}, {&ps, "x"}, {np, nil}, {nil, nil}} {
		if got := Deref(c.in); got != c.want {
			t.Errorf("Deref(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestStructs(t *testing.T) {
	a, b := row{1}, row{2}
	for name, in := range map[string]any{
		"struct": a, "ptr": &a, "slice": []row{a, b}, "ptr slice": &[]*row{&a, &b}, "array": [2]row{a, b},
	} {
		want := 1
		if name == "slice" || name == "ptr slice" || name == "array" {
			want = 2
		}
		if got := Structs(reflect.ValueOf(in)); len(got) != want {
			t.Errorf("%s: got %d structs, want %d", name, len(got), want)
		}
	}
	if got := Structs(reflect.ValueOf(map[string]any{})); got != nil {
		t.Errorf("map: got %v", got)
	}
	if got := Structs(reflect.ValueOf((*row)(nil))); got != nil {
		t.Errorf("nil ptr: got %v", got)
	}
}

func TestRepr(t *testing.T) {
	r := row{}
	if got := Repr(reflect.ValueOf(&r).Elem(), "fb"); got != "row!" {
		t.Errorf("addressable Stringer: %q", got)
	}
	if got := Repr(reflect.ValueOf(r), "fb"); got != "fb" {
		t.Errorf("unaddressable: %q", got)
	}
}

func TestEmit(t *testing.T) {
	var mem memory.Store
	ctx := witness.WithMeta(context.Background(), witness.Meta{Actor: "u", RemoteAddr: "1.1.1.1"})
	o := witness.Options{Mask: []string{"pw"}}
	a := &Snap{ID: "1", Repr: "r", Fields: map[string]any{"name": "a", "pw": "x"}}
	b := &Snap{ID: "1", Repr: "r", Fields: map[string]any{"name": "b", "pw": "x"}}

	for _, c := range []struct {
		act      witness.Action
		old, new *Snap
	}{
		{witness.Create, nil, a},
		{witness.Update, a, b},
		{witness.Update, a, a}, // no-op: writes nothing
		{witness.Delete, b, nil},
	} {
		if err := Emit(ctx, &mem, "t", c.act, o, c.old, c.new); err != nil {
			t.Fatal(err)
		}
	}
	got := mem.Entries()
	if len(got) != 3 {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	if got[0].Action != witness.Create || got[0].Changes["name"].New != "a" || got[0].Changes["pw"].New != "****" {
		t.Errorf("create: %+v", got[0])
	}
	if c := got[1].Changes["name"]; got[1].Action != witness.Update || c.Old != "a" || c.New != "b" || len(got[1].Changes) != 1 {
		t.Errorf("update: %+v", got[1])
	}
	if got[2].Action != witness.Delete || got[2].Changes["name"].Old != "b" || got[2].Changes["name"].New != nil {
		t.Errorf("delete: %+v", got[2])
	}
	for _, e := range got {
		if e.ObjectType != "t" || e.ObjectID != "1" || e.ObjectRepr != "r" || e.Actor != "u" || e.RemoteAddr != "1.1.1.1" || e.Timestamp.IsZero() {
			t.Errorf("metadata: %+v", e)
		}
	}
}
