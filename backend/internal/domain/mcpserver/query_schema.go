package mcpserver

import (
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Query predicates are recursive. The MCP SDK's Go type inference rejects that
// cycle; use the same explicit reference graph as the generated OpenAPI contract.
// Register these tools with Server.AddTool and decode original arguments: the
// typed helper's schema/default pass round-trips numbers through float64.
func queryTool(name, description string) *mcp.Tool {
	return queryToolSchema(name, description, "UnsV2QueryReq")
}

func operationQueryTool(d toolDefinition) *mcp.Tool {
	op, ok := findOperation(catalog(), d.operation)
	if !ok {
		panic("missing query contract: " + d.operation)
	}
	return queryToolSchema(d.name, d.description, op.Request)
}

func queryToolSchema(name, description, requestSchema string) *mcp.Tool {
	tool := readOnlyTool(name, description)
	contract := catalog()
	raw, err := json.Marshal(schemaClosure(contract.Schemas, requestSchema))
	if err != nil {
		panic(err)
	}
	var definitions map[string]any
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), "#/components/schemas/", "#/$defs/")), &definitions); err != nil {
		panic(err)
	}
	jsonSchemaNullable(definitions)
	root := object(definitions[requestSchema])
	delete(definitions, requestSchema)
	root["$defs"] = definitions
	tool.InputSchema = root
	return tool
}

func jobsQueryTool(name, description string) *mcp.Tool {
	tool := queryTool(name, description)
	properties := object(object(tool.InputSchema)["properties"])
	properties["resultOffset"] = map[string]any{"type": "integer", "minimum": 0}
	properties["resultSize"] = map[string]any{"type": "integer", "minimum": 0, "maximum": 200}
	return tool
}
