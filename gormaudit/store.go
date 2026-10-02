package gormaudit

import (
	"context"
	"errors"
	"time"

	"github.com/shubhodeep9/witness"
)

// LogEntry is the table row for a witness.Entry (django-auditlog's LogEntry).
// Create the table with db.AutoMigrate(&gormaudit.LogEntry{}) and query it like any model.
type LogEntry struct {
	ID         uint `gorm:"primaryKey"`
	Timestamp  time.Time
	Action     witness.Action
	ObjectType string `gorm:"index:idx_witness_object"`
	ObjectID   string `gorm:"index:idx_witness_object"`
	ObjectRepr string
	Changes    map[string]witness.Change `gorm:"serializer:json"`
	Actor      string                    `gorm:"index"`
	RemoteAddr string
	Additional map[string]any `gorm:"serializer:json"`
}

func (LogEntry) TableName() string { return "witness_log_entries" }

// Entry converts a stored row back to a witness.Entry. JSON round-tripping turns numbers
// in Changes into float64.
func (l LogEntry) Entry() witness.Entry {
	return witness.Entry{
		ObjectType: l.ObjectType, ObjectID: l.ObjectID, ObjectRepr: l.ObjectRepr,
		Action: l.Action, Changes: l.Changes, Actor: l.Actor, RemoteAddr: l.RemoteAddr,
		Timestamp: l.Timestamp, Additional: l.Additional,
	}
}

// NewStore returns a witness.Store that inserts a LogEntry in the audited write's own
// transaction, so the entry and the change commit or roll back together. It only works
// behind the gormaudit plugin; do not register LogEntry with the Registry.
func NewStore() witness.Store { return dbStore{} }

type dbStore struct{}

func (dbStore) Write(ctx context.Context, e witness.Entry) error {
	tx := TxFrom(ctx)
	if tx == nil {
		return errors.New("gormaudit: store used outside a gormaudit write")
	}
	return tx.Create(&LogEntry{
		ObjectType: e.ObjectType, ObjectID: e.ObjectID, ObjectRepr: e.ObjectRepr,
		Action: e.Action, Changes: e.Changes, Actor: e.Actor, RemoteAddr: e.RemoteAddr,
		Timestamp: e.Timestamp, Additional: e.Additional,
	}).Error
}
