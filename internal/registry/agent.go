package registry

import (
	"fmt"
	"sync"

	"github.com/modularis/modularis/internal/domain"
)

// Registry is a thread-safe, in-memory store that tracks all agents that
// are currently connected and registered with the orchestrator.
type Registry struct {
	mu     sync.RWMutex
	agents map[string]*domain.Agent // keyed by agent ID
}

// New creates a new empty Registry.
func New() *Registry {
	return &Registry{
		agents: make(map[string]*domain.Agent),
	}
}

// Add stores an agent in the registry. Returns an error if the ID already
// exists.
func (r *Registry) Add(agent *domain.Agent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.agents[agent.ID]; exists {
		return fmt.Errorf("agent %q already registered", agent.ID)
	}
	r.agents[agent.ID] = agent
	return nil
}

// Remove deletes an agent by ID. It is a no-op if the agent does not exist.
func (r *Registry) Remove(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.agents, agentID)
}

// Get returns the agent with the given ID, or nil if not found.
func (r *Registry) Get(agentID string) *domain.Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[agentID]
}

// List returns a snapshot of all currently registered agents.
func (r *Registry) List() []*domain.Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*domain.Agent, 0, len(r.agents))
	for _, a := range r.agents {
		out = append(out, a)
	}
	return out
}
