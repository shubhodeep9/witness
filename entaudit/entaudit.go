// Package entaudit audits registered Ent entities with a mutation hook:
//
//	client.Use(entaudit.Hook(&reg, store))
//
// Register the generated entity type: reg.Register(&ent.User{}, opts). Entities are matched
// by type name (the mutation's Type()); diff keys are the entity's Go field names.
//
// Old and new values come from the generated client (reached through the mutation's
// Client method, so reads join the caller's transaction): updates and deletes cost one
// SELECT per affected row before, and updates one after. A failed audit write fails the
// operation; inside a transaction (client.Tx) the caller then rolls back. Outside one,
// the change is already committed.
//
// Limits: entities need a single ID field; ObjectType is the Ent type name, not the table.
package entaudit

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"entgo.io/ent"
	"github.com/shubhodeep9/witness"
)

type mutKey struct{}

// Mutation returns the ent.Mutation being audited, for use inside a Store. Assert it to your
// generated type's Client method to insert the audit row in the same transaction:
//
//	c := entaudit.Mutation(ctx).(interface{ Client() *gen.Client }).Client()
//
// Nil outside an entaudit write.
func Mutation(ctx context.Context) ent.Mutation {
	m, _ := ctx.Value(mutKey{}).(ent.Mutation)
	return m
}

type audit struct {
	reg   *witness.Registry
	store witness.Store
}

// Hook returns the mutation hook to pass to client.Use.
func Hook(reg *witness.Registry, store witness.Store) ent.Hook {
	a := audit{reg, store}
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			o, ok := a.reg.LookupName(m.Type())
			if !ok {
				return next.Mutate(ctx, m)
			}
			switch {
			case m.Op().Is(ent.OpCreate):
				return a.create(ctx, next, m, o)
			case m.Op().Is(ent.OpUpdate | ent.OpUpdateOne):
				return a.update(ctx, next, m, o)
			case m.Op().Is(ent.OpDelete | ent.OpDeleteOne):
				return a.delete(ctx, next, m, o)
			}
			return next.Mutate(ctx, m)
		})
	}
}

// snap is one entity's state, keyed by Go field name.
type snap struct {
	idv    reflect.Value
	id     string
	fields map[string]any
}

func (a audit) create(ctx context.Context, next ent.Mutator, m ent.Mutation, o witness.Options) (ent.Value, error) {
	v, err := next.Mutate(ctx, m)
	if err != nil {
		return v, err
	}
	if n, ok := take(reflect.ValueOf(v)); ok {
		err = a.emit(ctx, m, witness.Create, o, nil, &n)
	}
	return v, err
}

func (a audit) update(ctx context.Context, next ent.Mutator, m ent.Mutation, o witness.Options) (ent.Value, error) {
	old, err := loadOld(ctx, m)
	if err != nil {
		return nil, err
	}
	v, err := next.Mutate(ctx, m)
	if err != nil {
		return v, err
	}
	for i := range old {
		n, err := get(ctx, m, old[i].idv)
		if err != nil {
			return v, err
		}
		if err := a.emit(ctx, m, witness.Update, o, &old[i], &n); err != nil {
			return v, err
		}
	}
	return v, nil
}

func (a audit) delete(ctx context.Context, next ent.Mutator, m ent.Mutation, o witness.Options) (ent.Value, error) {
	old, err := loadOld(ctx, m)
	if err != nil {
		return nil, err
	}
	v, err := next.Mutate(ctx, m)
	if err != nil {
		return v, err
	}
	for i := range old {
		if err := a.emit(ctx, m, witness.Delete, o, &old[i], nil); err != nil {
			return v, err
		}
	}
	return v, nil
}

// loadOld snapshots every entity the mutation will touch, via the mutation's generated IDs and client.
func loadOld(ctx context.Context, m ent.Mutation) ([]snap, error) {
	out, err := call(reflect.ValueOf(m), "IDs", reflect.ValueOf(ctx))
	if err != nil {
		return nil, err
	}
	ids := out[0]
	rows := make([]snap, ids.Len())
	for i := range rows {
		if rows[i], err = get(ctx, m, ids.Index(i)); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// get loads one entity through the generated client: m.Client().<Type>.Get(ctx, id).
func get(ctx context.Context, m ent.Mutation, id reflect.Value) (snap, error) {
	out, err := call(reflect.ValueOf(m), "Client")
	if err != nil {
		return snap{}, err
	}
	c := out[0]
	if c.Kind() != reflect.Pointer || c.IsNil() || c.Elem().Kind() != reflect.Struct {
		return snap{}, fmt.Errorf("entaudit: unexpected client %s", c.Type())
	}
	ec := c.Elem().FieldByName(m.Type())
	if !ec.IsValid() {
		return snap{}, fmt.Errorf("entaudit: client has no %s field", m.Type())
	}
	out, err = call(ec, "Get", reflect.ValueOf(ctx), id)
	if err != nil {
		return snap{}, err
	}
	s, ok := take(out[0])
	if !ok {
		return snap{}, fmt.Errorf("entaudit: %s.Get returned %s", m.Type(), out[0].Type())
	}
	return s, nil
}

// call invokes a method by name; a trailing error result is returned as err.
func call(v reflect.Value, name string, args ...reflect.Value) ([]reflect.Value, error) {
	meth := v.MethodByName(name)
	if !meth.IsValid() {
		return nil, fmt.Errorf("entaudit: %s has no %s method (not an Ent-generated type?)", v.Type(), name)
	}
	out := meth.Call(args)
	if n := len(out); n > 0 {
		if err, _ := out[n-1].Interface().(error); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// take snapshots a generated entity struct (or pointer to one).
func take(v reflect.Value) (snap, bool) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return snap{}, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return snap{}, false
	}
	s := snap{fields: map[string]any{}}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() || f.Name == "Edges" {
			continue
		}
		if f.Name == "ID" {
			s.idv = v.Field(i)
			s.id = fmt.Sprint(s.idv.Interface())
		}
		s.fields[f.Name] = deref(v.Field(i).Interface())
	}
	return s, s.idv.IsValid()
}

// emit diffs old→new (either may be nil) and writes the entry.
func (a audit) emit(ctx context.Context, m ent.Mutation, act witness.Action, o witness.Options, old, new *snap) error {
	var of, nf map[string]any
	cur := new
	if old != nil {
		of, cur = old.fields, old
	}
	if new != nil {
		nf, cur = new.fields, new
	}
	changes := witness.Diff(of, nf, o)
	if act == witness.Update && len(changes) == 0 {
		return nil
	}
	meta := witness.MetaFrom(ctx)
	e := witness.Entry{
		ObjectType: m.Type(),
		ObjectID:   cur.id,
		ObjectRepr: m.Type() + " " + cur.id, // generated String() would leak masked fields
		Action:     act,
		Changes:    changes,
		Actor:      meta.Actor,
		RemoteAddr: meta.RemoteAddr,
		Timestamp:  time.Now(),
	}
	return a.store.Write(context.WithValue(ctx, mutKey{}, m), e)
}

// deref unwraps pointers so snapshots don't alias live structs and nil == absent.
func deref(v any) any {
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
