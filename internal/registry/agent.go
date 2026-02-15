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

// RegisterCapability adds or updates a capability for the given agent ID.
// It is invoked when an agent sends a capability_register message after
// WebSocket upgrade (the /agent/capability/register path). The agent's
// Capabilities slice is mutated in-place while holding the lock.
//
// This enables dynamic registration at runtime: when components/modules are
// loaded into an agent, they can announce new functions without restarting
// the agent or the WebSocket session.
//
// If a capability with the same Name already exists it is replaced;
// otherwise it is appended. Returns an error only if the agentID is unknown
// (e.g. unregistered or already cleaned up).
func (r *Registry) RegisterCapability(agentID string, cap domain.Capability) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	agent, exists := r.agents[agentID]
	if !exists {
		return fmt.Errorf("agent %q not found", agentID)
	}

	// Replace if a capability with this name already exists (idempotent
	// update). Otherwise append.
	for i, existing := range agent.Capabilities {
		if existing.Name == cap.Name {
			agent.Capabilities[i] = cap
			return nil
		}
	}
	// Append new capability.
	agent.Capabilities = append(agent.Capabilities, cap)
	return nil
}

// GetByName returns the first agent matching the name (assumes unique names;
// for production, add name→ID index if duplicates allowed). Used for
// command routing from clients.
func (r *Registry) GetByName(name string) *domain.Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, a := range r.agents {
		if a.Name == name {
			return a
		}
	}
	return nil
}
