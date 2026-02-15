package domain

import "time"

// Agent represents a registered execution agent.
type Agent struct {
	// ID is assigned by the orchestrator upon successful registration.
	ID string `json:"id"`

	// Name is the self-declared name provided by the agent during registration.
	Name string `json:"name"`

	// Capabilities is the set of capabilities this agent provides.
	Capabilities []Capability `json:"capabilities"`

	// RegisteredAt is the time the agent completed registration.
	RegisteredAt time.Time `json:"registered_at"`
}
