package main

import (
	"context"
	"errors"

	"github.com/shubhodeep9/witness"
	"github.com/shubhodeep9/witness/entaudit"
	"github.com/shubhodeep9/witness/examples/ent/ent"
)

// store writes entries to the AuditLog table. The generated mutation's Client() is
// transaction-aware, so inside client.Tx the row commits or rolls back with the change.
type store struct{}

func (store) Write(ctx context.Context, e witness.Entry) error {
	m, ok := entaudit.Mutation(ctx).(interface{ Client() *ent.Client })
	if !ok {
		return errors.New("store used outside an entaudit write")
	}
	return m.Client().AuditLog.Create().
		SetTimestamp(e.Timestamp).
		SetAction(string(e.Action)).
		SetObjectType(e.ObjectType).
		SetObjectID(e.ObjectID).
		SetObjectRepr(e.ObjectRepr).
		SetChanges(e.Changes).
		SetActor(e.Actor).
		SetRemoteAddr(e.RemoteAddr).
		Exec(ctx)
}
