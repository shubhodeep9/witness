package bunaudit

import (
	"errors"
	"reflect"
	"unsafe"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/schema"
)

// Bun exposes neither a query's connection (needed to join the caller's transaction)
// nor its WHERE clauses (needed to find the rows an UPDATE/DELETE will touch), so both
// are read reflectively from unexported fields.
// ponytail: tied to bun internals (conn, where, whereFields); TestInspect pins them,
// upgrade bun and rerun it. If the fields vanish, inspect errors and OnError fires.

type queryState struct {
	conn    bun.IConn // nil when the query has no explicit conn
	where   []schema.QueryWithSep
	wherePK bool // WherePK was called: the model's primary keys select the rows
}

func inspect(q bun.Query) (s queryState, err error) {
	v := reflect.ValueOf(q)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return s, errors.New("bunaudit: unsupported query type")
	}
	v = v.Elem()

	f, ok := unexported(v, "conn")
	if !ok {
		return s, errors.New("bunaudit: bun internals changed: no conn field")
	}
	s.conn, _ = f.Interface().(bun.IConn)

	if f, ok := unexported(v, "where"); ok { // inserts have none
		s.where, _ = f.Interface().([]schema.QueryWithSep)
		wf, ok := unexported(v, "whereFields")
		if !ok {
			return s, errors.New("bunaudit: bun internals changed: no whereFields field")
		}
		s.wherePK = wf.Len() > 0
	}
	return s, nil
}

func unexported(v reflect.Value, name string) (reflect.Value, bool) {
	f := v.FieldByName(name)
	if !f.IsValid() {
		return f, false
	}
	return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem(), true
}

// whereSQL replays the query's WHERE entries exactly as bun renders them.
func whereSQL(gen schema.QueryGen, where []schema.QueryWithSep) (string, error) {
	var b []byte
	var err error
	for i, w := range where {
		if i > 0 {
			b = append(b, w.Sep...)
		}
		if w.Query == "" {
			continue
		}
		b = append(b, '(')
		if b, err = w.AppendQuery(gen, b); err != nil {
			return "", err
		}
		b = append(b, ')')
	}
	return string(b), nil
}
