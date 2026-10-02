// Package schema is the Ent schema used by entaudit's tests.
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type User struct{ ent.Schema }

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.String("password").Default(""),
		field.Int("age").Default(0),
	}
}

// Note is never registered with the audit registry.
type Note struct{ ent.Schema }

func (Note) Fields() []ent.Field {
	return []ent.Field{field.String("title")}
}
