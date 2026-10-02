// Package bunaudit audits registered Bun models with a bun.QueryHook.
//
// Bun hooks cannot fail the audited query, so a failed audit write is reported to
// Hook.OnError and, inside a transaction, rolls that transaction back so Commit fails.
// Outside a transaction the change itself is already committed.
//
// Limits: single-column primary keys only; map-based inserts are not audited;
// updates cost two extra SELECTs (before and after) so the diff is exact.
package bunaudit

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"reflect"
	"time"

	"github.com/shubhodeep9/witness"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/schema"
)

type (
	stashKey struct{}
	connKey  struct{}
	connVal  struct {
		db   *bun.DB
		conn bun.IConn
	}
)

// Conn returns the DB and connection (the caller's transaction, if any) an audited
// write is running on, so a Store can insert its row atomically:
//
//	db, conn := bunaudit.Conn(ctx)
//	db.NewInsert().Conn(conn).Model(&row).Exec(ctx)
//
// Both are nil outside a bunaudit write.
func Conn(ctx context.Context) (*bun.DB, bun.IConn) {
	c, _ := ctx.Value(connKey{}).(connVal)
	return c.db, c.conn
}

// Hook is a bun.QueryHook: db.AddQueryHook(bunaudit.New(&reg, store)).
type Hook struct {
	reg   *witness.Registry
	store witness.Store
	// OnError receives audit failures. New sets it to log them; it may be replaced.
	OnError func(error)
}

var _ bun.QueryHook = (*Hook)(nil)

func New(reg *witness.Registry, store witness.Store) *Hook {
	return &Hook{reg: reg, store: store, OnError: func(err error) { log.Printf("witness: %v", err) }}
}

// snap is one row's state, keyed by Go field name.
type snap struct {
	pk       any
	id, repr string
	fields   map[string]any
}

// BeforeQuery stashes the rows an UPDATE or DELETE is about to touch.
func (h *Hook) BeforeQuery(ctx context.Context, ev *bun.QueryEvent) context.Context {
	if op := ev.Operation(); op != "UPDATE" && op != "DELETE" {
		return ctx
	}
	t, _, ok := h.target(ev)
	if !ok {
		return ctx
	}
	old, err := h.loadOld(ctx, ev, t)
	if err != nil {
		h.fail(ev, err)
		return ctx
	}
	if ev.Stash == nil {
		ev.Stash = map[any]any{}
	}
	ev.Stash[stashKey{}] = old
	return ctx
}

func (h *Hook) AfterQuery(ctx context.Context, ev *bun.QueryEvent) {
	op := ev.Operation()
	if ev.Err != nil || (op != "INSERT" && op != "UPDATE" && op != "DELETE") {
		return
	}
	t, o, ok := h.target(ev)
	if !ok {
		return
	}
	if err := h.audit(ctx, ev, t, o, op); err != nil {
		h.fail(ev, err)
	}
}

func (h *Hook) audit(ctx context.Context, ev *bun.QueryEvent, t *schema.Table, o witness.Options, op string) error {
	old, _ := ev.Stash[stashKey{}].([]snap)
	switch op {
	case "INSERT":
		for _, rv := range structs(reflect.ValueOf(ev.Model.Value())) {
			n := take(t, rv)
			if err := h.emit(ctx, ev, t, witness.Create, o, nil, &n); err != nil {
				return err
			}
		}
	case "UPDATE":
		if len(old) == 0 {
			return nil
		}
		ids := make([]any, len(old))
		for i, r := range old {
			ids[i] = r.pk
		}
		cur, err := h.load(ctx, ev, t, "", ids)
		if err != nil {
			return err
		}
		byID := make(map[string]snap, len(cur))
		for _, r := range cur {
			byID[r.id] = r
		}
		for i := range old {
			if n, ok := byID[old[i].id]; ok {
				if err := h.emit(ctx, ev, t, witness.Update, o, &old[i], &n); err != nil {
					return err
				}
			}
		}
	case "DELETE":
		for i := range old {
			if err := h.emit(ctx, ev, t, witness.Delete, o, &old[i], nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// target reports whether the event's model is an audited table.
func (h *Hook) target(ev *bun.QueryEvent) (*schema.Table, witness.Options, bool) {
	tm, ok := ev.Model.(bun.TableModel)
	if !ok {
		return nil, witness.Options{}, false
	}
	t := tm.Table()
	if len(t.PKs) != 1 {
		return nil, witness.Options{}, false
	}
	o, ok := h.reg.Lookup(reflect.Zero(t.Type).Interface())
	return t, o, ok
}

// loadOld reads the rows selected by the query's WHERE clauses and, with WherePK, its model's keys.
func (h *Hook) loadOld(ctx context.Context, ev *bun.QueryEvent, t *schema.Table) ([]snap, error) {
	st, err := inspect(ev.IQuery)
	if err != nil {
		return nil, err
	}
	var ids []any
	if st.wherePK {
		for _, rv := range structs(reflect.ValueOf(ev.Model.Value())) {
			if v := t.PKs[0].Value(rv); !v.IsZero() {
				ids = append(ids, v.Interface())
			}
		}
	}
	var where string
	if len(st.where) > 0 {
		if where, err = whereSQL(ev.DB.QueryGen(), st.where); err != nil {
			return nil, err
		}
	}
	if where == "" && len(ids) == 0 {
		return nil, nil // bun will reject or no-op the query itself
	}
	return h.load(ctx, ev, t, where, ids)
}

// load selects rows on the query's own connection so it sees the caller's transaction.
func (h *Hook) load(ctx context.Context, ev *bun.QueryEvent, t *schema.Table, where string, ids []any) ([]snap, error) {
	st, err := inspect(ev.IQuery)
	if err != nil {
		return nil, err
	}
	dest := reflect.New(reflect.SliceOf(t.Type))
	q := ev.DB.NewSelect().Conn(connOf(ev, st)).Model(dest.Interface())
	if where != "" {
		q = q.Where("?", bun.Safe(where))
	}
	if len(ids) > 0 {
		q = q.Where("? IN (?)", bun.Ident(t.PKs[0].Name), bun.In(ids))
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	rows := dest.Elem()
	out := make([]snap, rows.Len())
	for i := range out {
		out[i] = take(t, rows.Index(i))
	}
	return out, nil
}

func connOf(ev *bun.QueryEvent, st queryState) bun.IConn {
	if st.conn != nil {
		return st.conn
	}
	return ev.DB.DB
}

// emit diffs old→new (either may be nil) and writes the entry.
func (h *Hook) emit(ctx context.Context, ev *bun.QueryEvent, t *schema.Table, a witness.Action, o witness.Options, old, new *snap) error {
	var of, nf map[string]any
	cur := new
	if old != nil {
		of, cur = old.fields, old
	}
	if new != nil {
		nf, cur = new.fields, new
	}
	changes := witness.Diff(of, nf, o)
	if a == witness.Update && len(changes) == 0 {
		return nil
	}
	st, err := inspect(ev.IQuery)
	if err != nil {
		return err
	}
	m := witness.MetaFrom(ctx)
	e := witness.Entry{
		ObjectType: t.Name,
		ObjectID:   cur.id,
		ObjectRepr: cur.repr,
		Action:     a,
		Changes:    changes,
		Actor:      m.Actor,
		RemoteAddr: m.RemoteAddr,
		Timestamp:  time.Now(),
	}
	return h.store.Write(context.WithValue(ctx, connKey{}, connVal{ev.DB, connOf(ev, st)}), e)
}

// fail reports err and poisons the surrounding transaction so the change cannot commit.
func (h *Hook) fail(ev *bun.QueryEvent, err error) {
	if h.OnError != nil {
		h.OnError(err)
	}
	if st, ierr := inspect(ev.IQuery); ierr == nil {
		if tx, ok := st.conn.(*sql.Tx); ok {
			_ = tx.Rollback()
		}
	}
}

// structs flattens a struct, pointer, slice or array (of values or pointers) into struct values.
func structs(rv reflect.Value) []reflect.Value {
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
	return nil
}

func take(t *schema.Table, rv reflect.Value) snap {
	fields := make(map[string]any, len(t.Fields))
	for _, f := range t.Fields {
		fields[f.GoName] = deref(f.Value(rv).Interface())
	}
	pk := t.PKs[0].Value(rv).Interface()
	id := fmt.Sprint(pk)
	repr := t.Name + " " + id
	if rv.CanAddr() {
		if st, ok := rv.Addr().Interface().(fmt.Stringer); ok {
			repr = st.String()
		}
	}
	return snap{pk: pk, id: id, repr: repr, fields: fields}
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
