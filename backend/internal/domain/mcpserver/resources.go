package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"unicode"

	"backend/internal/contextx"
	"backend/internal/domain/unsv2"
	"backend/internal/mcpaccess"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func resourceToolName(name string) string {
	var out strings.Builder
	out.WriteString("uns_")
	for _, character := range name {
		if unicode.IsUpper(character) {
			out.WriteByte('_')
		}
		out.WriteRune(unicode.ToLower(character))
	}
	return out.String()
}

func resourceExecutable(ctx context.Context, operation unsv2.ResourceOperation) bool {
	// Mixed metadata batches authorize deletion per item; ordinary delete tools
	// should only be advertised when deletion itself has been consented.
	if operation.Resource == "metadata" && operation.Destructive && operation.Name != "metadataBulkWrite" && operation.Name != "metadataTransact" {
		subject, _ := contextx.SubjectFrom(ctx)
		if subject.MCPRestricted && !mcpaccess.Has(subject, "uns.delete") {
			return false
		}
	}
	return !retiredDataQueryOperation(operation.Name) && permitted(ctx, "uns.read") && (operation.MCP == nil || *operation.MCP) && unsv2.ResourceAllowed(ctx, operation)
}

func (s *Service) addResourceTools(server *mcp.Server, subject contextx.Subject) {
	ctx := contextx.WithSubject(context.Background(), subject)
	contract := catalog()
	for _, operation := range unsv2.ResourceOperations() {
		if !resourceExecutable(ctx, operation) {
			continue
		}
		description := operation.Summary + ". Uses the shared OpenAPI contract. Read configuration is redacted; omitted patch fields are preserved."
		if operation.Resource == "record" || operation.Resource == "sample" {
			description = operation.Summary + ". Uses collection-scoped resource actions. Query fields are bare business names and _id/_meta system fields; ordinary Record/Sample results wrap business values in data. Aggregate output follows the pipeline. Sample find/aggregate require from/to and accept multiple recordIds; latest/delete require explicit recordIds. Protected credential fields cannot be queried or returned."
		}
		tool := readOnlyTool(resourceToolName(operation.Name), description)
		if operation.Mutation != "" {
			description = operation.Summary + ". Use the declared request schema; dryRun defaults to false where supported, and true performs rollback validation. Inspect partial outcomes and every result; only retry corrected failed items as a new request. Optional idempotencyKey and revision guards apply where declared; preserve the original key and unchanged request after an uncertain outcome."
			if operation.Resource == "record" {
				description = operation.Summary + ". Uses the current collection-scoped request/response contract; dryRun is available only where declared. Single-record update/replace supports explicit upsert with _id equality or a complete ready unique-index key. Deletes are soft and require separate uns.delete consent; restore accepts multiple ids and rechecks uniqueness atomically. Lock/renew/unlock use returned lease tokens; force unlock requires additional management permission. Batch insertedIds/upsertedIds and writeErrors preserve original zero-based input indexes. Inspect status/replayed and partial results; retry uncertain writes with the original key and unchanged request."
			}
			tool = operationTool(tool.Name, description, operation.Destructive)
			tool.Annotations.IdempotentHint = false
		}
		raw, _ := json.Marshal(schemaClosure(contract.Schemas, operation.Request))
		var definitions map[string]any
		_ = json.Unmarshal([]byte(strings.ReplaceAll(string(raw), "#/components/schemas/", "#/$defs/")), &definitions)
		jsonSchemaNullable(definitions)
		root := object(definitions[operation.Request])
		delete(definitions, operation.Request)
		root["$defs"] = definitions
		tool.InputSchema = root
		server.AddTool(tool, func(callCtx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			callCtx = contextx.WithSubject(callCtx, subject)
			if !resourceExecutable(callCtx, operation) {
				return result(nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "Resource operation is not authorized"})
			}
			if operation.Resource == "source" && operation.Mutation != "" && operation.Name != "sourceDocumentCreate" && operation.Name != "sourceNativeCredentialsUpdate" && operation.Name != "sourceNativeOAuthComplete" && credentialMaterial(request.Params.Arguments) {
				return result(nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "Use the dedicated Source credential API for secrets"})
			}
			value, err := s.uns.Execute(dataQueryReadContext(callCtx), operation.Name, request.Params.Arguments)
			if err != nil {
				return result(nil, safeMutationError(err))
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return result(nil, err)
			}
			var decoded any
			decoder := json.NewDecoder(bytes.NewReader(encoded))
			decoder.UseNumber()
			if err = decoder.Decode(&decoded); err != nil {
				return result(nil, err)
			}
			if operation.Name == "recordAggregatePipeline" || operation.Name == "sampleAggregate" || operation.Name == "metadataAggregate" {
				// Pipeline aliases can be kind/data/binding; they are business
				// documents and must never be interpreted as metadata envelopes.
				decoded = redactData(decoded)
			} else if strings.Contains(operation.Suffix, "/bindings/") {
				decoded = redactResourceConfiguration(decoded)
			} else if strings.Contains(operation.Suffix, "/sources/documents/") {
				decoded = sanitizeSourceDocuments(decoded)
			} else {
				decoded = sanitize(decoded)
			}
			if outcome, ok := decoded.(map[string]any); ok && outcome["summary"] != nil && outcome["jobId"] != nil {
				pageJobResults(decoded, 0, 20)
			}
			return result(decoded, nil)
		})
	}
}

// Current Source documents are already authorized by the domain serializer.
// Preserve its opaque credential reference for edit round trips, while keeping
// ordinary data, parameters and all secret material under the generic scrubber.
func sanitizeSourceDocuments(value any) any {
	refs := map[string]string{}
	var visit func(any, bool)
	visit = func(value any, restore bool) {
		switch node := value.(type) {
		case []any:
			for _, child := range node {
				visit(child, restore)
			}
		case map[string]any:
			if id, ok := node["id"].(string); ok && node["revision"] != nil {
				if document, ok := node["document"].(map[string]any); ok && document["protocol"] != nil && document["endpoint"] != nil {
					if restore {
						if ref := refs[id]; ref != "" {
							document["credentialRef"] = ref
						}
					} else if ref, ok := document["credentialRef"].(string); ok && ref != "" {
						refs[id] = ref
					}
				}
			}
			// Traverse only response containers, never arbitrary Source parameters.
			for _, key := range []string{"list", "items", "results"} {
				if child, ok := node[key]; ok {
					visit(child, restore)
				}
			}
		}
	}
	visit(value, false)
	value = sanitize(value)
	visit(value, true)
	return value
}

func redactResourceConfiguration(value any) any {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if key == "error" {
				node[key] = sanitize(child)
			} else if sensitive(key) || key == "headers" || key == "body" || key == "code" || key == "script" {
				node[key] = "[REDACTED]"
			} else {
				node[key] = redactResourceConfiguration(child)
			}
		}
	case []any:
		for index, child := range node {
			node[index] = redactResourceConfiguration(child)
		}
	case string:
		if parsed, err := url.Parse(node); err == nil && parsed.Host != "" && parsed.Scheme != "" {
			parsed.User = nil
			parsed.RawQuery = ""
			parsed.Fragment = ""
			return parsed.String()
		}
	}
	return value
}
