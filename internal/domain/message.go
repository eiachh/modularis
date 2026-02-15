package domain

import "encoding/json"

// -----------------------------------------------------------------------
// WebSocket message envelope
// -----------------------------------------------------------------------

// MessageType identifies the kind of WebSocket message.
type MessageType string

const (
	// MessageTypeRegister is sent by an agent as its first message after
	// the WebSocket connection is established.
	MessageTypeRegister MessageType = "register"

	// MessageTypeRegisterAck is sent by the orchestrator to confirm a
	// successful registration.
	MessageTypeRegisterAck MessageType = "register_ack"

	// MessageTypeError is sent by the orchestrator when an error occurs.
	MessageTypeError MessageType = "error"

	// MessageTypeDisplayRegister is sent by a display module as its first
	// message after the WebSocket connection is established.
	MessageTypeDisplayRegister MessageType = "display_register"

	// MessageTypeDisplayRegisterAck is sent by the orchestrator to confirm
	// a successful display registration.
	MessageTypeDisplayRegisterAck MessageType = "display_register_ack"

	// MessageTypeDisplay is sent by an agent to the orchestrator when it
	// wants to push output to all connected display modules. The
	// orchestrator fans this message out to every registered display.
	MessageTypeDisplay MessageType = "display"
)

// Envelope is the top-level wrapper for every WebSocket message exchanged
// between the orchestrator and an agent or display.
type Envelope struct {
	// Type discriminates the payload kind.
	Type MessageType `json:"type"`

	// Payload carries the type-specific data as raw JSON so it can be
	// decoded into the appropriate struct after inspecting Type.
	Payload json.RawMessage `json:"payload"`
}

// -----------------------------------------------------------------------
// Agent payloads
// -----------------------------------------------------------------------

// RegisterPayload is the payload for a MessageTypeRegister message.
// It is the first message an agent must send after connecting.
type RegisterPayload struct {
	// Name is a human-readable identifier chosen by the agent.
	Name string `json:"name"`

	// Capabilities lists everything this agent can execute.
	Capabilities []Capability `json:"capabilities"`
}

// RegisterAckPayload is returned by the orchestrator on successful
// registration.
type RegisterAckPayload struct {
	// AgentID is the orchestrator-assigned unique identifier.
	AgentID string `json:"agent_id"`
}

// -----------------------------------------------------------------------
// Display payloads
// -----------------------------------------------------------------------

// DisplayRegisterPayload is the payload for a MessageTypeDisplayRegister
// message. It is the first message a display module must send after
// connecting.
type DisplayRegisterPayload struct {
	// Name is a human-readable label chosen by the display module.
	Name string `json:"name"`

	// Type classifies the display (e.g. "terminal", "web", "led", "discord").
	Type string `json:"type"`
}

// DisplayRegisterAckPayload is returned by the orchestrator on successful
// display registration.
type DisplayRegisterAckPayload struct {
	// DisplayID is the orchestrator-assigned unique identifier.
	DisplayID string `json:"display_id"`
}

// DisplayPayload is sent by an agent when it wants to push output to
// display modules. The orchestrator broadcasts this to all connected
// displays.
type DisplayPayload struct {
	// AgentID identifies which agent produced this output.
	AgentID string `json:"agent_id"`

	// AgentName is the human-readable name of the agent.
	AgentName string `json:"agent_name"`

	// Title is a short summary of what happened (e.g. capability name).
	Title string `json:"title"`

	// Body is the main content to render.
	Body string `json:"body"`

	// Level indicates severity/importance (info, warn, error, success).
	Level string `json:"level"`
}

// -----------------------------------------------------------------------
// Shared payloads
// -----------------------------------------------------------------------

// ErrorPayload carries a machine-readable code and a human-readable
// description of an error.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
