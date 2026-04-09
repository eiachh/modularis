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
