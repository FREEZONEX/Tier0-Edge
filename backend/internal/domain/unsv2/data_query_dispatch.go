package unsv2

import (
	"context"
	"encoding/json"

	"backend/internal/contextx"
)

// Existing Record platform screens still submit the internal query DTO. Sample
// query always uses DataQueries so session callers cannot bypass recordId scope.
// Public callers
// and MCP always use the pipeline contract; legacy fields must never be silently
// accepted or discarded there. A batch cannot mix the two query languages.
func internalLegacyDataQuery(ctx context.Context, raw []byte) bool {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || subject.AuthType != "session" || subject.MCPRestricted {
		return false
	}
	var request struct {
		Queries []map[string]json.RawMessage `json:"queries"`
	}
	if json.Unmarshal(raw, &request) != nil {
		return false
	}
	for _, query := range request.Queries {
		if _, exists := query["pipeline"]; exists {
			return false
		}
		if _, exists := query["resultMode"]; exists {
			return false
		}
		if page := query["page"]; len(page) > 0 && page[0] == '{' {
			return false
		}
	}
	return true
}
