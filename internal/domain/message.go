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

	// MessageTypeCapabilityRegister is sent by an agent *after* the initial
	// "register" and WebSocket upgrade to dynamically register (or update)
	// a capability at runtime. This provides the streamlined /agent/capability/register
	// path requested, supporting dynamic component loading where capabilities
	// become known only after startup. The payload must follow the format:
	//   { "agent_name": "...", "function_name": "...", "schema": <json-schema> }
	MessageTypeCapabilityRegister MessageType = "capability_register"

	// MessageTypeCapabilityRegisterAck is sent by the orchestrator to
	// confirm that a runtime capability registration succeeded.
	MessageTypeCapabilityRegisterAck MessageType = "capability_register_ack"

	// MessageTypeCommand is sent by the orchestrator to an agent over
	// WebSocket when a client invokes a capability (e.g., via /invoke).
	// The agent processes it (e.g., echo) and may reply or broadcast display.
	MessageTypeCommand MessageType = "command"

	// MessageTypeCommandResult is optionally sent by the agent back to
	// orchestrator after command execution (for logging/return to client).
	MessageTypeCommandResult MessageType = "command_result"

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
// Capabilities are now registered exclusively at runtime via
// capability_register (no legacy support for initial caps).
type RegisterPayload struct {
	// Name is a human-readable identifier chosen by the agent.
	Name string `json:"name"`
}

// RegisterAckPayload is returned by the orchestrator on successful
// registration.
type RegisterAckPayload struct {
	// AgentID is the orchestrator-assigned unique identifier.
	AgentID string `json:"agent_id"`
}

// CapabilityRegisterPayload is the payload for a MessageTypeCapabilityRegister
// message. Per the request, the agent presents:
//   - its name
//   - the function (capability) name
//   - the JSON schema containing the required arguments for that function.
// This format enables runtime registration of capabilities after the agent
// has upgraded to WebSocket on /connect.
type CapabilityRegisterPayload struct {
	// AgentName is the human-readable name chosen by the agent at initial
	// registration (included for explicit identification even though the
	// WebSocket connection already ties the agent to its ID).
	AgentName string `json:"agent_name"`

	// FunctionName identifies the capability being registered (acts as the
	// capability.Name). Use namespaced strings for uniqueness
	// (e.g. "math.add", "gpio.write", "runtime-example").
	FunctionName string `json:"function_name"`

	// Schema is the JSON Schema describing the input arguments required by
	// the capability. It should be an object schema that enumerates
	// properties, types, and the "required" array. This satisfies the
	// "json schema that is containing the capabilities required argument"
	// requirement and enables rich, standard validation.
	//
	// Example:
	//   {
	//     "type": "object",
	//     "properties": {
	//       "message": { "type": "string" }
	//     },
	//     "required": ["message"]
	//   }
	Schema json.RawMessage `json:"schema"`
}

// CapabilityRegisterAckPayload is returned by the orchestrator to
// acknowledge successful runtime capability registration.
type CapabilityRegisterAckPayload struct {
	// CapabilityName echoes back the registered function_name so the agent
	// can correlate the ack.
	CapabilityName string `json:"capability_name"`
}

// -----------------------------------------------------------------------
// REST API payloads (clients)
// -----------------------------------------------------------------------

// -----------------------------------------------------------------------
// Command payloads (client → orchestrator → agent)
// -----------------------------------------------------------------------

// CapabilitySummary is the flattened format returned by GET /capabilities.
// It exposes each registered capability as: agent name, function name, and
// the JSON schema for required arguments (no legacy ParameterDef).
// (Internal; public subset in pkg if needed.)
type CapabilitySummary struct {
	// AgentName is the name of the owning agent.
	AgentName string `json:"agent_name"`

	// FunctionName is the capability's unique name (from function_name
	// during registration).
	FunctionName string `json:"function_name"`

	// Schema is the JSON Schema for this capability's required arguments
	// (copied from input_schema at registration time).
	Schema json.RawMessage `json:"schema"`
}

// CommandPayload is used *internally* by orchestrator to forward a client
// invocation to an agent's WebSocket connection (adds AgentID). Public
// equivalent for clients is pkg.InvokeCommand (in pkg/ for third-party use).
type CommandPayload struct {
	// AgentID is resolved by orchestrator from name (for WS routing).
	AgentID string `json:"agent_id"`
	// AgentName for logging/echo to display.
	AgentName string `json:"agent_name"`
	// FunctionName identifies the capability to invoke (e.g., "echo").
	FunctionName string `json:"function_name"`
	// Args is the validated input matching the capability's schema.
	Args json.RawMessage `json:"args"`
}

// CommandResultPayload is optionally returned by agent after execution.
// For echo, we primarily use display broadcast instead.
// (Internal; public in use via pkg if expanded.)
type CommandResultPayload struct {
	// Success indicates if the command executed without error.
	Success bool `json:"success"`
	// Result holds output (e.g., echoed message).
	Result string `json:"result,omitempty"`
	// Error describes failure (if any).
	Error string `json:"error,omitempty"`
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
