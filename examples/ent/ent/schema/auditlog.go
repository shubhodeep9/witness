package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/shubhodeep9/witness"
)

// AuditLog stores witness entries. It is never registered with the witness Registry.
type AuditLog struct{ ent.Schema }

func (AuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.Time("timestamp"),
		field.String("action"),
		field.String("object_type"),
		field.String("object_id"),
		field.String("object_repr"),
		field.JSON("changes", map[string]witness.Change{}),
		field.String("actor").Default(""),
		field.String("remote_addr").Default(""),
	}
}
