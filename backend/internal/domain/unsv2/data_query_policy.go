package unsv2

import (
	"strings"

	"backend/internal/domain/unsmeta"
	"backend/internal/mcpaccess"
)

// CheckMCPDataQueryField validates field provenance before filtering, aliasing
// or aggregating can hide a credential value behind an innocuous output name.
// The compiler invokes this policy from the persisted MCP identity, so worker
// retries recheck the current schema without depending on a context callback.
func CheckMCPDataQueryField(path string, schema *unsmeta.Schema) error {
	parts, err := unsmeta.ParseFieldPath(strings.TrimPrefix(path, "$"))
	if err != nil {
		return pipelineError("invalid field path")
	}
	for _, part := range parts {
		if mcpaccess.IsCredentialField(part) {
			return &Fault{Code: "FORBIDDEN", Path: "pipeline", Message: "Protected credential fields cannot participate in MCP data queries"}
		}
	}
	return checkMCPDataSchema(schema, 0)
}

func checkMCPDataSchema(schema *unsmeta.Schema, depth int) error {
	if schema == nil {
		return nil
	}
	if depth > 32 {
		return &Fault{Code: "INVALID_REQUEST", Path: "pipeline", Message: "Field schema nesting limit exceeded"}
	}
	// Open objects can contain undeclared credentials. Returning a redacted
	// document is safe, but grouping/filtering the entire object is not.
	if schema.Type == "object" && (schema.AdditionalProperties == nil || *schema.AdditionalProperties) {
		return &Fault{Code: "FORBIDDEN", Path: "pipeline", Message: "MCP queries must reference declared scalar fields or closed objects without credential fields"}
	}
	if schema.Type == "array" && schema.Items == nil {
		return &Fault{Code: "FORBIDDEN", Path: "pipeline", Message: "MCP queries cannot operate on untyped arrays"}
	}
	for key, child := range schema.Properties {
		if err := CheckMCPDataQueryField(unsmeta.FormatFieldPath([]string{key}), nil); err != nil {
			return err
		}
		if err := checkMCPDataSchema(&child, depth+1); err != nil {
			return err
		}
	}
	return checkMCPDataSchema(schema.Items, depth+1)
}
