// Package gormaudit audits registered GORM models via create/update/delete callbacks.
//
// Limits: models need a primary key; map-based creates (Create(&map)) are not audited;
// updates cost two extra SELECTs (before and after) so the diff is exact.
package gormaudit

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/shubhodeep9/witness"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

type txKey struct{}

// TxFrom returns a clean session on the transaction an audited write is running in,
// so a Store can insert its row atomically. Nil outside a gormaudit write.
func TxFrom(ctx context.Context) *gorm.DB {
	tx, _ := ctx.Value(txKey{}).(*gorm.DB)
	return tx
}

type plugin struct {
	reg   *witness.Registry
	store witness.Store
}

// New returns a GORM plugin: db.Use(gormaudit.New(&reg, store)).
func New(reg *witness.Registry, store witness.Store) gorm.Plugin {
	return &plugin{reg, store}
}

func (p *plugin) Name() string { return "witness" }

func (p *plugin) Initialize(db *gorm.DB) error {
	cb := db.Callback()
	const commit = "gorm:commit_or_rollback_transaction"
	// after-callbacks must run before commit so a store error rolls the write back
	return errors.Join(
		cb.Create().After("gorm:after_create").Before(commit).Register("witness:create", p.afterCreate),
		cb.Update().After("gorm:setup_reflect_value").Before("gorm:update").Register("witness:before_update", p.loadOld),
		cb.Update().After("gorm:after_update").Before(commit).Register("witness:update", p.afterUpdate),
		cb.Delete().After("gorm:before_delete").Before("gorm:delete").Register("witness:before_delete", p.loadOld),
		cb.Delete().After("gorm:after_delete").Before(commit).Register("witness:delete", p.afterDelete),
	)
}

const oldKey = "witness:old"

// snap is one row's state, keyed by Go field name.
type snap struct {
	pk       any
	id, repr string
	fields   map[string]any
}

// opts reports whether this statement's model is audited.
func (p *plugin) opts(db *gorm.DB) (witness.Options, bool) {
	s := db.Statement.Schema
	if db.Error != nil || s == nil || s.PrioritizedPrimaryField == nil {
		return witness.Options{}, false
	}
	return p.reg.Lookup(reflect.Zero(s.ModelType).Interface())
}

func (p *plugin) afterCreate(db *gorm.DB) {
	o, ok := p.opts(db)
	if !ok {
		return
	}
	for _, rv := range structs(db.Statement.ReflectValue) {
		n := take(db.Statement.Schema, rv)
		p.emit(db, witness.Create, o, nil, &n)
	}
}

// loadOld stashes the rows about to be updated or deleted.
func (p *plugin) loadOld(db *gorm.DB) {
	if _, ok := p.opts(db); !ok {
		return
	}
	s, f := db.Statement.Schema, db.Statement.Schema.PrioritizedPrimaryField
	q := db.Session(&gorm.Session{NewDB: true})
	hasWhere := false
	if c, ok := db.Statement.Clauses["WHERE"]; ok {
		q, hasWhere = q.Clauses(c.Expression), true
	}
	var ids []any
	for _, rv := range structs(db.Statement.ReflectValue) {
		if v, zero := f.ValueOf(db.Statement.Context, rv); !zero {
			ids = append(ids, v)
		}
	}
	if len(ids) > 0 {
		q = q.Where(clause.IN{Column: clause.Column{Name: f.DBName}, Values: ids})
	} else if !hasWhere {
		return // GORM will reject the global write itself
	}
	old, err := load(q, s)
	if err != nil {
		db.AddError(err)
		return
	}
	db.InstanceSet(oldKey, old)
}

func (p *plugin) afterUpdate(db *gorm.DB) {
	o, ok := p.opts(db)
	old := oldRows(db)
	if !ok || len(old) == 0 {
		return
	}
	s, f := db.Statement.Schema, db.Statement.Schema.PrioritizedPrimaryField
	ids := make([]any, len(old))
	for i, r := range old {
		ids[i] = r.pk
	}
	q := db.Session(&gorm.Session{NewDB: true}).Where(clause.IN{Column: clause.Column{Name: f.DBName}, Values: ids})
	cur, err := load(q, s)
	if err != nil {
		db.AddError(err)
		return
	}
	byID := make(map[string]snap, len(cur))
	for _, r := range cur {
		byID[r.id] = r
	}
	for i := range old {
		if n, ok := byID[old[i].id]; ok {
			p.emit(db, witness.Update, o, &old[i], &n)
		}
	}
}

func (p *plugin) afterDelete(db *gorm.DB) {
	o, ok := p.opts(db)
	if !ok || db.RowsAffected == 0 {
		return
	}
	old := oldRows(db)
	for i := range old {
		p.emit(db, witness.Delete, o, &old[i], nil)
	}
}

func oldRows(db *gorm.DB) []snap {
	v, _ := db.InstanceGet(oldKey)
	rows, _ := v.([]snap)
	return rows
}

// emit diffs old→new (either may be nil) and writes the entry; a store error fails the write.
func (p *plugin) emit(db *gorm.DB, a witness.Action, o witness.Options, old, new *snap) {
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
		return
	}
	ctx := db.Statement.Context
	m := witness.MetaFrom(ctx)
	e := witness.Entry{
		ObjectType: db.Statement.Schema.Table,
		ObjectID:   cur.id,
		ObjectRepr: cur.repr,
		Action:     a,
		Changes:    changes,
		Actor:      m.Actor,
		RemoteAddr: m.RemoteAddr,
		Timestamp:  time.Now(),
	}
	ctx = context.WithValue(ctx, txKey{}, db.Session(&gorm.Session{NewDB: true}))
	if err := p.store.Write(ctx, e); err != nil {
		db.AddError(err)
	}
}

// structs flattens a struct, slice or array (of values or pointers) into struct values.
func structs(rv reflect.Value) []reflect.Value {
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

func load(q *gorm.DB, s *schema.Schema) ([]snap, error) {
	rows := reflect.New(reflect.SliceOf(s.ModelType))
	if err := q.Find(rows.Interface()).Error; err != nil {
		return nil, err
	}
	rv := rows.Elem()
	out := make([]snap, rv.Len())
	for i := range out {
		out[i] = take(s, rv.Index(i))
	}
	return out, nil
}

func take(s *schema.Schema, rv reflect.Value) snap {
	ctx := context.Background()
	fields := make(map[string]any, len(s.Fields))
	for _, f := range s.Fields {
		if f.DBName == "" { // relations, ignored fields
			continue
		}
		v, _ := f.ValueOf(ctx, rv)
		fields[f.Name] = deref(v)
	}
	pk, _ := s.PrioritizedPrimaryField.ValueOf(ctx, rv)
	id := fmt.Sprint(pk)
	repr := s.Table + " " + id
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
