package policy

// Authorize returns true if the given identity is allowed to invoke
// (serviceID, capability). Default effect is deny.
func (s *Store) Authorize(identity, serviceID, capability string) bool {
	p := s.GetPolicy(identity)
	if p == nil {
		return false // no policy → deny
	}
	rules := collectRules(s, p)
	eff := resolveEffect(rules, serviceID, capability)
	return eff == EffectAllow
}

// collectRules gathers all rules applicable to a policy:
// 1. Rules from each role in p.Roles
// 2. Rules directly in p.Rules
func collectRules(s *Store, p *Policy) []interface {
	GetServiceID() string
	GetCapability() string
	GetEffect() Effect
} {
	var out []interface {
		GetServiceID() string
		GetCapability() string
		GetEffect() Effect
	}
	// From roles
	for _, rn := range p.Roles {
		if r := s.GetRole(rn); r != nil {
			for i := range r.Rules {
				out = append(out, roleRuleAdapter{&r.Rules[i]})
			}
		}
	}
	// Direct rules
	for i := range p.Rules {
		out = append(out, ruleAdapter{&p.Rules[i]})
	}
	return out
}

// resolveEffect returns the effective effect for (svc, cap).
// Last matching rule wins. If none match, returns EffectDeny.
func resolveEffect(rules []interface {
	GetServiceID() string
	GetCapability() string
	GetEffect() Effect
}, serviceID, capability string) Effect {
	var eff Effect = EffectDeny
	for _, r := range rules {
		if r.GetServiceID() == serviceID && r.GetCapability() == capability {
			eff = r.GetEffect()
		}
	}
	return eff
}

// --- Adapters so RoleRule and Rule share the same interface ---

type roleRuleAdapter struct{ *RoleRule }
type ruleAdapter struct{ *Rule }

func (a roleRuleAdapter) GetServiceID() string { return a.ServiceID }
func (a roleRuleAdapter) GetCapability() string { return a.Capability }
func (a roleRuleAdapter) GetEffect() Effect { return a.Effect }

func (a ruleAdapter) GetServiceID() string { return a.ServiceID }
func (a ruleAdapter) GetCapability() string { return a.Capability }
func (a ruleAdapter) GetEffect() Effect { return a.Effect }
