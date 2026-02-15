package pkg

import (
	"encoding/json"
	"fmt"
	"strings"

	// gojsonschema for JSON Schema validation (dep in go.mod; reusable
	// util in pkg for third parties/clients).
	"github.com/xeipuuv/gojsonschema"
)

// ValidateArgsAgainstSchema validates pre-marshaled args JSON (json.RawMessage
// or []byte) against the provided JSON schema (RawMessage, typically from a
// CapabilitySummary or InvokeCommand).
//
// This util is exported in pkg/utils.go for third-party/client reuse (e.g.,
// before sending commands). Uses gojsonschema for full JSON Schema
// compliance (required fields, types, etc.). Returns detailed error on
// failure. Accepts RawMessage for backend efficiency (no re-marshal).
//
// On client: marshal args , extract schema from stored caps , pass here (see
// AssembleAndValidateCommand). Backend (/invoke) passes raw args directly.
func ValidateArgsAgainstSchema(argsJSON, schema json.RawMessage) error {
	// Validate using gojsonschema (argsJSON assumed valid bytes from marshal/bind).
	schemaLoader := gojsonschema.NewBytesLoader(schema)
	docLoader := gojsonschema.NewBytesLoader(argsJSON)
	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		return fmt.Errorf("schema validation error: %w", err)
	}
	if !result.Valid() {
		// Collect errors for helpful output (e.g., missing required).
		var errs []string
		for _, e := range result.Errors() {
			errs = append(errs, e.String())
		}
		return fmt.Errorf("invalid args: %s", strings.Join(errs, "; "))
	}
	return nil
}