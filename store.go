package witness

import "context"

// Store persists entries. ORM adapters implement it so the write can join the caller's transaction.
type Store interface {
	Write(ctx context.Context, e Entry) error
}
