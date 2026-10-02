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

	"github.com/shubhodeep9/witness"
	"github.com/shubhodeep9/witness/internal/audit"
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
	for _, rv := range audit.Structs(db.Statement.ReflectValue) {
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
	for _, rv := range audit.Structs(db.Statement.ReflectValue) {
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
		ids[i] = r.Key
	}
	q := db.Session(&gorm.Session{NewDB: true}).Where(clause.IN{Column: clause.Column{Name: f.DBName}, Values: ids})
	cur, err := load(q, s)
	if err != nil {
		db.AddError(err)
		return
	}
	byID := make(map[string]audit.Snap, len(cur))
	for _, r := range cur {
		byID[r.ID] = r
	}
	for i := range old {
		if n, ok := byID[old[i].ID]; ok {
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

func oldRows(db *gorm.DB) []audit.Snap {
	v, _ := db.InstanceGet(oldKey)
	rows, _ := v.([]audit.Snap)
	return rows
}

// emit writes the entry for one row; a store error fails the write.
func (p *plugin) emit(db *gorm.DB, a witness.Action, o witness.Options, old, new *audit.Snap) {
	ctx := context.WithValue(db.Statement.Context, txKey{}, db.Session(&gorm.Session{NewDB: true}))
	if err := audit.Emit(ctx, p.store, db.Statement.Schema.Table, a, o, old, new); err != nil {
		db.AddError(err)
	}
}

func load(q *gorm.DB, s *schema.Schema) ([]audit.Snap, error) {
	rows := reflect.New(reflect.SliceOf(s.ModelType))
	if err := q.Find(rows.Interface()).Error; err != nil {
		return nil, err
	}
	rv := rows.Elem()
	out := make([]audit.Snap, rv.Len())
	for i := range out {
		out[i] = take(s, rv.Index(i))
	}
	return out, nil
}

func take(s *schema.Schema, rv reflect.Value) audit.Snap {
	ctx := context.Background()
	fields := make(map[string]any, len(s.Fields))
	for _, f := range s.Fields {
		if f.DBName == "" { // relations, ignored fields
			continue
		}
		v, _ := f.ValueOf(ctx, rv)
		fields[f.Name] = audit.Deref(v)
	}
	pk, _ := s.PrioritizedPrimaryField.ValueOf(ctx, rv)
	id := fmt.Sprint(pk)
	return audit.Snap{Key: pk, ID: id, Repr: audit.Repr(rv, s.Table+" "+id), Fields: fields}
}
