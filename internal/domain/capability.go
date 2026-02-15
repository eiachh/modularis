package domain

// Capability describes a single action an agent can perform.
type Capability struct {
	// Name is the unique identifier for this capability (e.g. "math.add", "gpio.write").
	Name string `json:"name"`

	// Description is a human-readable summary of what the capability does.
	Description string `json:"description"`

	// Parameters describes the expected input schema as a list of parameter definitions.
	Parameters []ParameterDef `json:"parameters,omitempty"`
}

// ParameterDef describes a single input parameter for a capability.
type ParameterDef struct {
	// Name of the parameter.
	Name string `json:"name"`

	// Type is the JSON-schema-style type (string, number, boolean, object, array).
	Type string `json:"type"`

	// Required indicates whether this parameter must be provided.
	Required bool `json:"required"`

	// Description is a human-readable explanation of the parameter.
	Description string `json:"description,omitempty"`
}
