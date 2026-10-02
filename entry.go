// Package witness is an audit log library modelled on django-auditlog.
package witness

import "time"

type Action string

const (
	Create Action = "create"
	Update Action = "update"
	Delete Action = "delete"
	Access Action = "access"
)

// Change is one field's before/after value. Old is nil on create, New is nil on delete.
type Change struct {
	Old any `json:"old"`
	New any `json:"new"`
}

// Entry is the django-auditlog LogEntry equivalent.
type Entry struct {
	ObjectType string
	ObjectID   string
	ObjectRepr string
	Action     Action
	Changes    map[string]Change
	Actor      string
	RemoteAddr string
	Timestamp  time.Time
	Additional map[string]any
}
