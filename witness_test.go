package witness

import "testing"

type user struct{ Name string }

func TestRegistry(t *testing.T) {
	var r Registry
	if _, ok := r.Lookup(user{}); ok {
		t.Fatal("zero registry should have nothing")
	}

	r.Register(&user{}, Options{Mask: []string{"Name"}})
	for _, m := range []any{user{}, &user{}, new(*user)} {
		o, ok := r.Lookup(m)
		if !ok || len(o.Mask) != 1 {
			t.Fatalf("lookup %T: got %+v, %v", m, o, ok)
		}
	}

	if _, ok := r.Lookup(struct{}{}); ok {
		t.Fatal("unregistered type found")
	}
}
