package bunaudit

import (
	"context"
	"errors"
	"time"

	"github.com/shubhodeep9/witness"
	"github.com/uptrace/bun"
)

// LogEntry is the table row for a witness.Entry (django-auditlog's LogEntry).
// Create the table with db.NewCreateTable().Model((*bunaudit.LogEntry)(nil)).Exec(ctx); add
// indexes on (object_type, object_id) and actor yourself if you query by them.
type LogEntry struct {
	bun.BaseModel `bun:"table:witness_log_entries"`

	ID         int64 `bun:",pk,autoincrement"`
	Timestamp  time.Time
	Action     witness.Action
	ObjectType string
	ObjectID   string
	ObjectRepr string
	Changes    map[string]witness.Change
	Actor      string
	RemoteAddr string
	Additional map[string]any
}

// Entry converts a stored row back to a witness.Entry. JSON round-tripping turns numbers
// in Changes into float64.
func (l LogEntry) Entry() witness.Entry {
	return witness.Entry{
		ObjectType: l.ObjectType, ObjectID: l.ObjectID, ObjectRepr: l.ObjectRepr,
		Action: l.Action, Changes: l.Changes, Actor: l.Actor, RemoteAddr: l.RemoteAddr,
		Timestamp: l.Timestamp, Additional: l.Additional,
	}
}

// NewStore returns a witness.Store that inserts a LogEntry on the audited write's own
// connection (the caller's transaction, if any), so the entry and the change commit or
// roll back together. It only works behind the bunaudit hook; do not register LogEntry with
// the Registry.
func NewStore() witness.Store { return dbStore{} }

type dbStore struct{}

func (dbStore) Write(ctx context.Context, e witness.Entry) error {
	db, conn := Conn(ctx)
	if db == nil {
		return errors.New("bunaudit: store used outside a bunaudit write")
	}
	_, err := db.NewInsert().Conn(conn).Model(&LogEntry{
		ObjectType: e.ObjectType, ObjectID: e.ObjectID, ObjectRepr: e.ObjectRepr,
		Action: e.Action, Changes: e.Changes, Actor: e.Actor, RemoteAddr: e.RemoteAddr,
		Timestamp: e.Timestamp, Additional: e.Additional,
	}).Exec(ctx)
	return err
}
