package policy

// Effect is the result of an authorization check.
type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

// Role is a reusable bundle of permission rules.
type Role struct {
	Name  string     `json:"name"`
	Rules []RoleRule `json:"rules"`
}

// RoleRule grants or denies a capability on a service within a role.
type RoleRule struct {
	ServiceID  string `json:"service_id"`
	Capability string `json:"capability"`
	Effect     Effect `json:"effect"`
}

// Policy binds an identity to roles and optional direct rules.
type Policy struct {
	Identity string `json:"identity"`
	Roles    []string `json:"roles"`
	Rules    []Rule   `json:"rules"`
}

// Rule is a direct permission rule for an identity (overrides role rules).
type Rule struct {
	ServiceID  string `json:"service_id"`
	Capability string `json:"capability"`
	Effect     Effect `json:"effect"`
}

// Grant allows a delegatee identity to act on behalf of a delegator identity
// for specific capabilities. This enables capability token delegation.
type Grant struct {
	// Delegator is the identity that created this grant (the one whose
	// permissions will be used when the delegatee invokes capabilities).
	Delegator string `json:"delegator"`

	// Delegatee is the identity that receives this grant (can act as delegator).
	Delegatee string `json:"delegatee"`

	// TargetAgent is the agent/service this grant applies to.
	// Use "*" for wildcard (any agent).
	TargetAgent string `json:"target_agent"`

	// TargetCapability is the capability this grant applies to.
	// Use "*" for wildcard (any capability on the target agent).
	TargetCapability string `json:"target_capability"`

	// ExpiresAt is an optional Unix timestamp when this grant expires.
	// Zero means no expiry.
	ExpiresAt int64 `json:"expires_at,omitempty"`

	// CreatedAt is the Unix timestamp when this grant was created.
	CreatedAt int64 `json:"created_at"`
}
