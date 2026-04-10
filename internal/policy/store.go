package policy

import (
	"sync"
	"time"
)

// Store holds all roles, policies, and grants in memory.
// Thread-safe.
type Store struct {
	mu       sync.RWMutex
	roles    map[string]*Role
	policies map[string]*Policy
	grants   []Grant // stored as a slice, indexed by delegatee via GetGrants
}

// NewStore creates an empty policy store.
func NewStore() *Store {
	return &Store{
		roles:    make(map[string]*Role),
		policies: make(map[string]*Policy),
		grants:   make([]Grant, 0),
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

// AddGrant adds a new grant to the store.
// Returns the created grant with CreatedAt set.
func (s *Store) AddGrant(g Grant) Grant {
	s.mu.Lock()
	defer s.mu.Unlock()
	g.CreatedAt = time.Now().UTC().Unix()
	s.grants = append(s.grants, g)
	return g
}

// GetGrants returns all grants where the given identity is the delegatee.
// Grants are filtered to exclude expired ones (if ExpiresAt > 0 and has passed).
func (s *Store) GetGrants(delegatee string) []Grant {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now().UTC().Unix()
	var result []Grant
	for _, g := range s.grants {
		if g.Delegatee == delegatee {
			// Skip expired grants
			if g.ExpiresAt > 0 && g.ExpiresAt < now {
				continue
			}
			result = append(result, g)
		}
	}
	return result
}

// ListGrants returns all grants (for SU admin listing).
func (s *Store) ListGrants() []Grant {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Grant, len(s.grants))
	copy(result, s.grants)
	return result
}

// RevokeGrant removes a grant matching the given criteria.
// Returns true if a grant was found and removed.
func (s *Store) RevokeGrant(delegator, delegatee, targetAgent, targetCapability string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, g := range s.grants {
		if g.Delegator == delegator &&
			g.Delegatee == delegatee &&
			g.TargetAgent == targetAgent &&
			g.TargetCapability == targetCapability {
			// Remove by swapping with last and truncating
			s.grants[i] = s.grants[len(s.grants)-1]
			s.grants = s.grants[:len(s.grants)-1]
			return true
		}
	}
	return false
}
