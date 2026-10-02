package witness

import (
	"context"
	"reflect"
	"testing"
)

func TestDiff(t *testing.T) {
	old := map[string]any{"name": "a", "age": 1, "pw": "secret", "same": 1}
	new := map[string]any{"name": "b", "age": 1, "pw": "hunter2", "same": 1, "skip": 9}

	got := Diff(old, new, Options{Exclude: []string{"skip"}, Mask: []string{"pw"}})
	want := map[string]Change{
		"name": {"a", "b"},
		"pw":   {"****", "****"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("update: got %v want %v", got, want)
	}

	got = Diff(nil, map[string]any{"name": "a", "age": 1}, Options{Include: []string{"name"}})
	want = map[string]Change{"name": {nil, "a"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("create: got %v want %v", got, want)
	}

	if d := Diff(old, old, Options{}); len(d) != 0 {
		t.Fatalf("no-op: got %v", d)
	}
}

func TestMeta(t *testing.T) {
	ctx := WithMeta(context.Background(), Meta{Actor: "u1", RemoteAddr: "1.2.3.4"})
	if m := MetaFrom(ctx); m.Actor != "u1" || m.RemoteAddr != "1.2.3.4" {
		t.Fatalf("got %+v", m)
	}
	if m := MetaFrom(context.Background()); m != (Meta{}) {
		t.Fatalf("got %+v", m)
	}
}
