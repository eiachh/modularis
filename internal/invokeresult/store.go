package invokeresult

import (
	"encoding/json"
	"sync"
)

// Result holds the final result data for an invocation.
// It is set when the agent sends command_result (or an acknowledgment).
type Result struct {
	CapabilityID string          `json:"capability_id"`
	Success      bool            `json:"success"`
	Result       json.RawMessage `json:"result,omitempty"`
	Error        string          `json:"error,omitempty"`
}

// Entry holds a result and a channel that closes when the result is ready.
type Entry struct {
	ready  chan struct{}
	result *Result
}

// Store is a thread-safe store for invocation results.
// Clients can wait for a result by blocking on Wait().
type Store struct {
	mu      sync.Mutex
	entries map[string]*Entry
}

// New creates an empty result store.
func New() *Store {
	return &Store{
		entries: make(map[string]*Entry),
	}
}

// Create reserves an entry for the given invocation ID and returns it.
// If an entry already exists, it is returned (idempotent).
func (s *Store) Create(invocationID string) *Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.entries[invocationID]; ok {
		return e
	}
	e := &Entry{ready: make(chan struct{})}
	s.entries[invocationID] = e
	return e
}

// Get returns the entry for an invocation ID, or nil if not found.
func (s *Store) Get(invocationID string) *Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[invocationID]
}

// Set stores the result for the given invocation ID and signals waiters.
// If no entry exists, it is created first.
func (s *Store) Set(invocationID string, r *Result) {
	s.mu.Lock()
	e, ok := s.entries[invocationID]
	if !ok {
		e = &Entry{ready: make(chan struct{})}
		s.entries[invocationID] = e
	}
	e.result = r
	s.mu.Unlock()
	close(e.ready)
}

// Result returns the stored result, or nil if not yet set.
func (e *Entry) Result() *Result {
	return e.result
}

// Wait blocks until the result is ready.
func (e *Entry) Wait() {
	<-e.ready
}
