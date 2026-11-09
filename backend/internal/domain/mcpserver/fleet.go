package mcpserver

import (
	"backend/internal/contextx"
	"backend/internal/domain/fleetapi"
	"backend/internal/domain/unsv2"
	"backend/internal/mcpaccess"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Service) addFleetTools(server *mcp.Server, subject contextx.Subject) {
	if s.fleet == nil || subject.APIKeyAppID != "" {
		return
	}
	for _, contract := range fleetapi.Contracts() {
		permission, scope := fleetapi.OperationAccess(contract.Name, subject)
		if !contextx.HasResource(subject, permission) || (subject.MCPRestricted && !mcpaccess.Has(subject, scope)) {
			continue
		}
		op := contract
		var schema map[string]any
		if json.Unmarshal(op.Schema, &schema) != nil {
			continue
		}
		tool := operationTool("fleet_"+op.Name, op.Description, op.Name == "edgeSourcesDelete")
		tool.InputSchema = schema
		server.AddTool(tool, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx = contextx.WithSubject(ctx, subject)
			if op.Name == "runtimeExecute" {
				var runtime unsv2.FleetRuntimeRequest
				if err := unsv2.Decode(request.Params.Arguments, &runtime); err != nil {
					return result(nil, err)
				}
				// A remote wrapper must retain the native MCP contract's explicit
				// execution boundary, including for API-key-authenticated Agents.
				if native, ok := unsv2.FindResourceOperation(runtime.Operation); ok && native.MCP != nil && !*native.MCP {
					return result(nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "This operation is not executable through MCP"})
				}
			}
			value, err := s.fleet.Execute(ctx, op.Name, request.Params.Arguments)
			if err != nil {
				return result(nil, safeMutationError(err))
			}
			return result(value, nil)
		})
	}
}
