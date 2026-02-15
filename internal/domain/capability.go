package domain

import "encoding/json"

// Capability describes a single action an agent can perform.
// Capabilities are registered either at connect time (via RegisterPayload)
// or dynamically at runtime (via capability_register message). The latter
// enables components to be loaded and advertise their functions without
// restarting/reconnecting the agent.
type Capability struct {
	// Name is the unique identifier for this capability (e.g. "math.add", "gpio.write").
	Name string `json:"name"`

	// Description is a human-readable summary of what the capability does.
	Description string `json:"description"`

	// InputSchema describes the expected input arguments using a JSON Schema
	// document. This replaces the former Parameters[] list to provide a
	// standard, expressive way to declare required arguments, types,
	// validation rules, defaults, etc.
	//
	// Example:
	//   {
	//     "type": "object",
	//     "properties": {
	//       "message": {
	//         "type": "string",
	//         "description": "The message to echo back"
	//       }
	//     },
	//     "required": ["message"]
	//   }
	//
	// The schema is typically an object type whose properties define the
	// capability's parameters. This aligns with the request for JSON-schema-
	// based argument description in the /agent/capability/register flow.
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}
