package policy

import (
	"sync"
)

// Store holds all roles and policies in memory.
// Thread-safe.
type Store struct {
	mu       sync.RWMutex
	roles    map[string]*Role
	policies map[string]*Policy
}

// NewStore creates an empty policy store.
func NewStore() *Store {
	return &Store{
		roles:    make(map[string]*Role),
		policies: make(map[string]*Policy),
	}
}

// GetRole returns a role by name, or nil if not found.
func (s *Store) GetRole(name string) *Role {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.roles[name]
}

// SetRole inserts or replaces a role.
func (s *Store) SetRole(r *Role) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r != nil {
		s.roles[r.Name] = r
	}
}

// ListRoles returns all roles (snapshot).
func (s *Store) ListRoles() []*Role {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Role, 0, len(s.roles))
	for _, r := range s.roles {
		out = append(out, r)
	}
	return out
}

// GetPolicy returns the policy for an identity, or nil if none.
func (s *Store) GetPolicy(identity string) *Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.policies[identity]
}

// SetPolicy inserts or replaces a policy for an identity.
func (s *Store) SetPolicy(p *Policy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p != nil {
		s.policies[p.Identity] = p
	}
}

// ListPolicies returns all policies (snapshot).
func (s *Store) ListPolicies() []*Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Policy, 0, len(s.policies))
	for _, p := range s.policies {
		out = append(out, p)
	}
	return out
}

// EnsurePolicy creates an empty policy for identity if none exists.
// Used for WS agents on registration.
func (s *Store) EnsurePolicy(identity string) *Policy {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.policies[identity]; ok {
		return p
	}
	p := &Policy{Identity: identity, Roles: nil, Rules: nil}
	s.policies[identity] = p
	return p
}
