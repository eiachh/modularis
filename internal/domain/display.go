package domain

import "time"

// Display represents a registered display module.
// Displays are output adapters that receive events from the orchestrator
// and render them (terminal, web UI, LEDs, messaging platforms, etc.).
type Display struct {
	// ID is assigned by the orchestrator upon successful registration.
	ID string `json:"id"`

	// Name is a human-readable label chosen by the display module.
	Name string `json:"name"`

	// Type classifies the display (e.g. "terminal", "web", "led", "discord").
	Type string `json:"type"`

	// RegisteredAt is the time the display completed registration.
	RegisteredAt time.Time `json:"registered_at"`
}
