package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"backend/internal/domain/unsv2"
)

func (s *Service) examples(ctx context.Context, input ExampleInput) (any, error) {
	if !permitted(ctx, "uns.read") {
		return nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "Read permission required"}
	}
	if input.Client != "curl" && input.Client != "sdk" && input.Client != "cli" && input.Client != "mcp" || len(input.Operations) == 0 || len(input.Operations) > 20 {
		return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Message: "client=curl/sdk/cli/mcp and 1..20 operations required"}
	}
	out := []map[string]any{}
	if input.Scenario != "" && input.Scenario != "linked" && input.Scenario != "patch" {
		return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Path: "scenario", Reason: "Use linked or patch", Repair: "Omit scenario for basic per-operation examples"}
	}

	c := catalog()
	for _, name := range input.Operations {
		o, ok := findOperation(c, name)
		if !ok {
			return nil, s.operationFault(ctx, name)
		}
		if retiredDataQueryOperation(o.Name) {
			return nil, &unsv2.Fault{Code: "UNSUPPORTED_OPERATION", Path: "operations", Message: "Use the current collection-scoped Record or Sample resource examples"}
		}
		if !operationDocumentationAllowed(ctx, o.Name) {
			return nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "App model read permission required"}
		}
		if o.CoreOnly {
			return nil, &unsv2.Fault{Code: "UNSUPPORTED_OPERATION", Path: "operations", Reason: "This operation is internal-only and has no OpenAPI/MCP call contract", Repair: "Use the supported public query operations returned by api_operations_list"}
		}
		body := exampleBody(o.Name)
		if o.Example != nil {
			raw, _ := json.Marshal(o.Example)
			_ = json.Unmarshal(raw, &body)
		}
		if input.Scenario != "" {
			if o.Name != "metadataTransact" {
				return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Path: "scenario", Message: "linked and patch scenarios use metadataTransact"}
			}
			if input.Scenario == "linked" {
				body = map[string]any{"dryRun": true, "operations": []any{
					map[string]any{"opId": "root", "action": "insertOne", "kind": "directory", "data": map[string]any{"name": "Workshop"}},
					map[string]any{"opId": "child", "action": "insertOne", "kind": "directory", "dependsOn": []string{"root"}, "data": map[string]any{"name": "Line 1", "parentId": map[string]any{"ref": "root"}}},
				}}
			} else {
				body = map[string]any{"dryRun": true, "operations": []any{map[string]any{"opId": "rename", "action": "updateOne", "kind": "directory", "id": "<DIRECTORY_ID>", "update": map[string]any{"$set": map[string]any{"description": "Updated description"}}}}}
			}
		}
		path := "/openapi/v2" + o.Suffix
		if input.Client != "mcp" && len(o.PathParams) > 0 {
			fields, _ := body.(map[string]any)
			for _, key := range o.PathParams {
				value, _ := fields[key].(string)
				if value == "" {
					value = "<" + key + ">"
				}
				path = strings.ReplaceAll(path, ":"+key, url.PathEscape(value))
				delete(fields, key)
			}
		}
		raw, _ := json.MarshalIndent(body, "", "  ")
		code := ""
		switch input.Client {
		case "mcp":
			name := "documentation-only"
			for _, tool := range queryTools {
				if tool.operation == o.Name {
					name = tool.name
					break
				}
			}
			for _, tool := range scopedBatchTools {
				if tool.operation == o.Name {
					name = tool.name
					break
				}
			}
			for _, tool := range aiTools {
				if tool.operation == o.Name && !strings.HasPrefix(tool.description, "Deprecated") {
					name = tool.name
					break
				}
			}
			if o.Name == "jobs" {
				name = "uns_jobs_query"
			}
			if _, ok := unsv2.FindResourceOperation(o.Name); ok {
				name = resourceToolName(o.Name)
			}
			if !operationExecutable(ctx, o.Name) {
				name = "documentation-only"
			}
			encoded, _ := json.MarshalIndent(map[string]any{"name": name, "arguments": body}, "", "  ")
			code = string(encoded)
		case "curl":
			code = fmt.Sprintf("curl --request POST \"<INSTANCE_BASE_URL>%s\" --header \"Content-Type: application/json\" --header \"X-API-Key: <API_KEY>\" --data @request.json", path)
		case "cli":
			code = fmt.Sprintf("tier0 login --base-url <INSTANCE_BASE_URL>\ntier0 api %s --body-file request.json --json", path)
		case "sdk":
			code = fmt.Sprintf("import { configureClient, getClient } from '@tier0/sdk';\nconfigureClient({ apiHost: '<INSTANCE_BASE_URL>', apiKey: '<API_KEY>' });\nconst response = await getClient().post(%q, %s);", path, string(raw))
		}
		out = append(out, map[string]any{"operation": o.Name, "method": "POST", "path": path, "client": input.Client, "code": code, "requestFile": "request.json", "request": body, "mcpExecutable": operationExecutable(ctx, o.Name)})
	}
	return map[string]any{"productVersion": s.version, "examples": out, "sdk": map[string]any{"package": "@tier0/sdk", "verifiedSurface": "configureClient/getClient().post", "checkedVersion": "0.2.8", "nativeUnsV2ConvenienceMethods": false}, "notes": []string{"Replace URL/key/IDs locally; no actual key is copied by this service", "CLI uses its existing login/profile authentication", "Record transactions use explicit dryRun=true for rollback previews and false for intended commits on the same interface. Other resource actions support previews only where their request schema declares dryRun; previews do not reserve execution state", "Prefer dedicated resource tools for ordinary CUD; transaction tools are for related atomic operations", "MCP writes require explicit consented scopes; metadata writes default dryRun to false, while Record transactions require explicit dryRun; App credential/context APIs are documented only and have no MCP execution tool", "App context uses an App-bound data key; issuing/revoking App keys uses the existing platform credential-management permission"}}, nil
}

func exampleBody(operation string) any {
	query := map[string]any{"queryId": "query", "page": 1, "size": 20}
	switch operation {
	case "catalog", "appContext":
		return map[string]any{}
	case "appKeyCreate":
		return map[string]any{"appId": "<APP_ID>", "name": "Example App data access"}
	case "appKeyRevoke":
		return map[string]any{"appId": "<APP_ID>", "keyId": "<APP_KEY_ID>"}
	case "namespace":
		query["parentId"] = nil
		query["includeAncestors"] = true
	case "topics":
		query["topicId"] = "<TOPIC_ID>"
	case "references":
		query["targets"] = []any{map[string]any{"kind": "collection", "id": "<COLLECTION_ID>"}}
	case "jobs":
		query["jobIds"] = []string{"<JOB_ID>"}
	}
	return map[string]any{"queries": []any{query}}
}
