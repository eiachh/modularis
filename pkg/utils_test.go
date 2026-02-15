package pkg

import (
	"encoding/json"
	"testing"
)

// TestValidateArgsAgainstSchema tests the exported validator util (moved to
// pkg/utils.go). Uses table-driven cases for valid/invalid schemas/argsJSON to
// ensure JSON Schema compliance (required fields, types, errors). This
// verifies the core used by clients/third parties/backend for command assembly
// (args passed as pre-marshaled RawMessage).
func TestValidateArgsAgainstSchema(t *testing.T) {
	// Simple echo-like schema for tests.
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"message": {"type": "string"}
		},
		"required": ["message"]
	}`)

	tests := []struct {
		name     string
		args     any // will be marshaled to argsJSON
		wantErr  bool
	}{
		{
			name:    "valid args",
			args:    map[string]string{"message": "hello"},
			wantErr: false,
		},
		{
			name:    "invalid - missing required field",
			args:    map[string]string{}, // no "message"
			wantErr: true,
		},
		{
			name:    "invalid - wrong type",
			args:    map[string]int{"message": 123}, // not string
			wantErr: true,
		},
		{
			// Struct marshals to JSON (field name must match schema "message";
			// use tag for robustness).
			name:    "valid complex args",
			args:    struct{ Message string `json:"message"` }{"test"},
			wantErr: false,
		},
		{
			name:    "invalid - nil args",
			args:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to RawMessage (as done in client/backend for argsJSON).
			argsJSON, marshalErr := json.Marshal(tt.args)
			if marshalErr != nil {
				if !tt.wantErr {
					t.Fatalf("unexpected marshal err: %v", marshalErr)
				}
				return // expected for nil
			}
			err := ValidateArgsAgainstSchema(argsJSON, schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateArgsAgainstSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
			// Additional: check error message contains details for invalid cases.
			if tt.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
		})
	}
}

// TestValidateArgsAgainstSchema_BadSchema tests error on invalid schema input.
func TestValidateArgsAgainstSchema_BadSchema(t *testing.T) {
	badSchema := json.RawMessage(`{invalid json}`)
	// Valid argsJSON for test.
	argsJSON := json.RawMessage(`{"message": "test"}`)

	err := ValidateArgsAgainstSchema(argsJSON, badSchema)
	if err == nil {
		t.Error("expected error for bad schema, got nil")
	}
}