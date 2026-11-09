package platform

import (
	"encoding/json"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
)

var requestSchemasOnce sync.Once
var requestSchemas map[string]*jsonschema.Resolved
var requestSchemasError error

// The schema source is generated from the same contract as the public docs.
// Per-operation validation rejects missing required values, nulls, wrong types
// and irrelevant multipart fields before any domain operation can run.
func validatePlatformRequest(operation string, raw []byte) error {
	requestSchemasOnce.Do(func() {
		var definitions map[string]json.RawMessage
		if err := json.Unmarshal([]byte(PlatformV2RequestSchemasJSON), &definitions); err != nil {
			requestSchemasError = err
			return
		}
		requestSchemas = make(map[string]*jsonschema.Resolved, len(definitions))
		for name, data := range definitions {
			var schema jsonschema.Schema
			if err := json.Unmarshal(data, &schema); err != nil {
				requestSchemasError = err
				return
			}
			resolved, err := schema.Resolve(nil)
			if err != nil {
				requestSchemasError = err
				return
			}
			requestSchemas[name] = resolved
		}
	})
	if requestSchemasError != nil || requestSchemas[operation] == nil {
		return &apiError{500, "INTERNAL_ERROR", "Platform request contract is unavailable", ""}
	}
	var value map[string]any
	// JSON Schema's Go validator expects decoded JSON numbers, while the
	// operation DTO is decoded separately to exact bounded integer fields.
	if err := json.Unmarshal(raw, &value); err != nil {
		return invalid("", "A valid JSON object is required")
	}
	if err := requestSchemas[operation].Validate(value); err != nil {
		return invalid("", "Request does not match the documented operation schema; check required fields, types, ranges and operation-specific parameters")
	}
	return nil
}
