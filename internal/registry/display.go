package registry

import (
	"fmt"
	"sync"

	"github.com/modularis/modularis/internal/domain"
)

// DisplayRegistry is a thread-safe, in-memory store that tracks all display
// modules currently connected and registered with the orchestrator.
type DisplayRegistry struct {
	mu       sync.RWMutex
	displays map[string]*domain.Display // keyed by display ID
}

// NewDisplayRegistry creates a new empty DisplayRegistry.
func NewDisplayRegistry() *DisplayRegistry {
	return &DisplayRegistry{
		displays: make(map[string]*domain.Display),
	}
}

// Add stores a display in the registry. Returns an error if the ID already
// exists.
func (r *DisplayRegistry) Add(d *domain.Display) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.displays[d.ID]; exists {
		return fmt.Errorf("display %q already registered", d.ID)
	}
	r.displays[d.ID] = d
	return nil
}

// Remove deletes a display by ID. It is a no-op if the display does not exist.
func (r *DisplayRegistry) Remove(displayID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.displays, displayID)
}

// Get returns the display with the given ID, or nil if not found.
func (r *DisplayRegistry) Get(displayID string) *domain.Display {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.displays[displayID]
}

// List returns a snapshot of all currently registered displays.
func (r *DisplayRegistry) List() []*domain.Display {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*domain.Display, 0, len(r.displays))
	for _, d := range r.displays {
		out = append(out, d)
	}
	return out
}
