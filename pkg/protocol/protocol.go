package protocol

import "encoding/json"

// Envelope represents a WebSocket message envelope used by the orchestrator protocol.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// MustMarshal is a helper that panics on marshal error (for static data).
func MustMarshal(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}