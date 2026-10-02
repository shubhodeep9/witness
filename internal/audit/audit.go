// Package audit holds the helpers the ORM adapters share.
package audit

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/shubhodeep9/witness"
)

// Snap is one row's state, keyed by Go field name.
type Snap struct {
	Key    any    // raw primary key, for re-querying
	ID     string // Key as Entry.ObjectID
	Repr   string
	Fields map[string]any
}

// Emit diffs old→new (either may be nil: create has no old, delete no new) and writes the
// entry to s. An update that changed nothing writes nothing. ctx is passed to the store as is.
func Emit(ctx context.Context, s witness.Store, objectType string, a witness.Action, o witness.Options, old, new *Snap) error {
	var of, nf map[string]any
	cur := new
	if old != nil {
		of, cur = old.Fields, old
	}
	if new != nil {
		nf, cur = new.Fields, new
	}
	changes := witness.Diff(of, nf, o)
	if a == witness.Update && len(changes) == 0 {
		return nil
	}
	m := witness.MetaFrom(ctx)
	return s.Write(ctx, witness.Entry{
		ObjectType: objectType,
		ObjectID:   cur.ID,
		ObjectRepr: cur.Repr,
		Action:     a,
		Changes:    changes,
		Actor:      m.Actor,
		RemoteAddr: m.RemoteAddr,
		Timestamp:  time.Now(),
	})
}

// Repr is rv's String() if its pointer has one, else fallback.
func Repr(rv reflect.Value, fallback string) string {
	if rv.CanAddr() {
		if st, ok := rv.Addr().Interface().(fmt.Stringer); ok {
			return st.String()
		}
	}
	return fallback
}

// Structs flattens a struct, pointer, slice or array (of values or pointers) into struct values.
func Structs(rv reflect.Value) []reflect.Value {
	rv = reflect.Indirect(rv)
	switch rv.Kind() {
	case reflect.Struct:
		return []reflect.Value{rv}
	case reflect.Slice, reflect.Array:
		out := make([]reflect.Value, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			if e := reflect.Indirect(rv.Index(i)); e.Kind() == reflect.Struct {
				out = append(out, e)
			}
		}
		return out
	}
	return nil // maps etc.
}

// Deref unwraps pointers so snapshots don't alias live structs and nil == absent.
func Deref(v any) any {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}
	return rv.Interface()
}
