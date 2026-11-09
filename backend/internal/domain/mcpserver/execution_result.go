package mcpserver

import (
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
)

// Same machine envelope as Cloud MCP: one JSON value in text and structured
// content. Call records contain only routing and redacted outcome information.
func nativeExecutionResult(raw string, err error, unknown bool, calls []map[string]any) *mcp.CallToolResult {
	outcome := "ok"
	var failure any
	if err != nil {
		outcome = "failed"
		failure = map[string]any{"code": "MCP_EXECUTION_FAILED", "message": strings.ReplaceAll(err.Error(), "api_contract_get", "the current operation request schema with the MCP search tool")}
	}
	notices := []map[string]any{}
	if unknown {
		outcome = "unknown"
		notices = append(notices, map[string]any{"code": "OUTCOME_UNKNOWN", "message": "A write may have taken effect; inspect target state before retrying, retaining the original idempotency key."})
	}
	var value any
	if raw != "" {
		if json.Valid([]byte(raw)) {
			value = json.RawMessage(raw)
		} else {
			value = raw
		}
	}
	envelope := map[string]any{"contractVersion": "1.0", "result": value, "outcome": outcome, "error": failure, "calls": calls, "notices": notices}
	encoded, _ := json.Marshal(envelope)
	return &mcp.CallToolResult{IsError: err != nil, Content: []mcp.Content{&mcp.TextContent{Text: string(encoded)}}, StructuredContent: envelope}
}
