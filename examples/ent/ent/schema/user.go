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
		field.Int("updated_at").Default(0),
	}
}
