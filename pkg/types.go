package pkg

import "encoding/json"

// Package pkg contains exported types for the Modularis API, following Go
// conventions for reusability by third parties/clients/adapters (e.g.,
// import "github.com/modularis/modularis/pkg"). This keeps the public
// command interface separate from internal/domain. Utils (e.g.,
// validation) are in utils.go.

// InvokeCommand is the canonical "command send package" for invoking
// agent capabilities. Use this to:
// - Assemble commands client-side (after fetching capabilities and
//   validating via schema).
// - Send to orchestrator's /invoke endpoint.
// - (Orchestrator forwards internally to agents.)
//
// Agents register capabilities at runtime (name + JSON schema); clients
// must validate/assemble before sending. Only supports runtime-registered
// functions like "echo".
//
// This is the struct moved to pkg/ as requested for third-party availability.
type InvokeCommand struct {
	// AgentName targets the agent (must match a registered agent name).
	AgentName string `json:"agent_name"`
	// FunctionName is the capability to invoke (e.g., "echo"; checked
	// against orchestrator's registry).
	FunctionName string `json:"function_name"`
	// Args is validated JSON input matching the capability's schema
	// (required fields, types, etc.).
	Args json.RawMessage `json:"args"`
}