package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Agent represents a registered execution agent.
type Agent struct {
	// ID assigned by orchestrator on registration.
	ID string `json:"id"`

	// Name self-declared by agent.
	Name string `json:"name"`

	// Capabilities provided by agent (populated at runtime).
	Capabilities []Capability `json:"capabilities"`

	// RegisteredAt timestamp.
	RegisteredAt time.Time `json:"registered_at"`
}

// NewAgent creates a new Agent from registration name: generates ID,
// sets empty capabilities (for dynamic runtime reg), and stamps time.
func NewAgent(name string) *Agent {
	return &Agent{
		ID:           uuid.New().String(),
		Name:         name,
		Capabilities: nil, // empty until runtime registration
		RegisteredAt: time.Now().UTC(),
	}
}

// ValidateName returns error if name is empty (for register flows).
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name is required")
	}
	return nil
}
