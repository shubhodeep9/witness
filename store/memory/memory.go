// Package memory is an in-memory witness.Store for tests and examples.
package memory

import (
	"context"
	"sync"

	"github.com/shubhodeep9/witness"
)

type Store struct {
	mu      sync.Mutex
	entries []witness.Entry
}

func (s *Store) Write(_ context.Context, e witness.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	return nil
}

// Entries returns a copy of everything written so far, oldest first.
func (s *Store) Entries() []witness.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]witness.Entry(nil), s.entries...)
}
