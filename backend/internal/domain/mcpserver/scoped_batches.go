package mcpserver

import (
	"context"
	"encoding/json"
	"strings"

	"backend/internal/contextx"
	"backend/internal/domain/unsv2"
	"backend/internal/mcpaccess"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var scopedBatchTools = []toolDefinition{
	{name: "uns_records_transaction", operation: "recordTransactionExecute", description: "Execute one atomic business transaction across authorized Collections: query/check/read/create/update/replace/delete. Use dryRun=true to validate with rollback and dryRun=false to commit; preview does not reserve locks or execution state. record.query accepts filter (same fields as pipeline $match), sort and bounded limit. Bind id to query.records.N.id from the same Collection. Query records and read document use the canonical Record object; read data remains the business payload for bindings. Each Record may be written only once. No allowPartial or aggregate queries. Use dedicated collection resource actions for ordinary writes. Optional idempotencyKey deduplicates unchanged retries. Accepted jobs use uns_jobs_query. Delete requires separate uns.delete consent."},
}

func scopedBatchAllowed(ctx context.Context, operation string) bool {
	if _, exists := findOperation(catalog(), operation); !exists {
		return false
	}
	if !permitted(ctx, "uns.read") {
		return false
	}
	subject, _ := contextx.SubjectFrom(ctx)
	switch unsv2.BatchScope(operation) {
	case "records", "samples":
		return mcpaccess.Has(subject, "uns.write")
	}
	return false
}

func (s *Service) addScopedBatchTools(server *mcp.Server, subject contextx.Subject) {
	ctx := contextx.WithSubject(context.Background(), subject)
	contract := catalog()
	for _, definition := range scopedBatchTools {
		d := definition
		if !scopedBatchAllowed(ctx, d.operation) {
			continue
		}
		op, found := findOperation(contract, d.operation)
		if !found {
			continue
		}
		preview := strings.HasSuffix(d.operation, "Validate")
		tool := readOnlyTool(d.name, d.description)
		if !preview {
			tool = operationTool(d.name, d.description, true)
			tool.Annotations.IdempotentHint = false
		}
		raw, _ := json.Marshal(schemaClosure(contract.Schemas, op.Request))
		var definitions map[string]any
		_ = json.Unmarshal([]byte(strings.ReplaceAll(string(raw), "#/components/schemas/", "#/$defs/")), &definitions)
		jsonSchemaNullable(definitions)
		root := object(definitions[op.Request])
		delete(definitions, op.Request)
		root["$defs"] = definitions
		if d.operation == "recordTransactionExecute" {
			root["required"] = append(array(root["required"]), "dryRun")
		}
		tool.InputSchema = root
		server.AddTool(tool, func(callCtx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			callCtx = contextx.WithSubject(callCtx, subject)
			if !scopedBatchAllowed(callCtx, d.operation) {
				return result(nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "Required batch scope is missing"})
			}
			var input mutationInput
			if err := unsv2.Decode(request.Params.Arguments, &input); err != nil {
				return result(nil, err)
			}
			value, err := s.mutateScoped(callCtx, input, d.operation)
			return result(value, safeMutationError(err))
		})
	}
}
