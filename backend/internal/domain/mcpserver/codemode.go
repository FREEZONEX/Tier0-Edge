package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"backend/internal/contextx"
	"backend/internal/domain/common"
	"backend/internal/domain/fleetapi"
	"backend/internal/domain/unsv2"
	"backend/internal/mcpaccess"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type codeInput struct {
	Code string `json:"code" jsonschema:"JavaScript async arrow function"`
}
type executeInput struct {
	CredentialMaterial map[string]any `json:"credentialMaterial,omitempty" jsonschema:"Optional native credentials for one credentialImport session request; never exposed to JavaScript"`
	Code               string         `json:"code" jsonschema:"JavaScript async arrow function calling tier0.request"`
	WorkspaceID        string         `json:"workspaceId,omitempty" jsonschema:"Optional; must match whoami workspaceId (1 on Enterprise)"`
}
type codeRoute struct {
	operation     operation
	tool          string
	requestSchema map[string]any
}

func codePath(op operation) string {
	path := "/openapi/v2" + op.Suffix
	if op.CoreOnly {
		path = "/api/core/v2" + op.Suffix
	}
	for _, name := range op.PathParams {
		path = strings.ReplaceAll(path, ":"+name, "{"+name+"}")
	}
	return path
}

// Only already-authorized, registered operations are exposed. A supplied path
// is never used as a network URL, and the child never receives credentials.
func (s *Service) codeRoutes(ctx context.Context) []codeRoute {
	routes := []codeRoute{}
	for _, op := range catalog().Operations {
		if !operationExecutable(ctx, op.Name) || op.CoreOnly {
			continue
		}
		name := ""
		subject, _ := contextx.SubjectFrom(ctx)
		if subject.APIKeyAppID != "" {
			_, resource := unsv2.FindResourceOperation(op.Name)
			if !resource && op.Name != "dataDiscover" && op.Name != "collectionDescribe" {
				continue
			}
		}
		if _, ok := unsv2.FindResourceOperation(op.Name); ok {
			name = resourceToolName(op.Name)
		}
		for _, d := range queryTools {
			if d.operation == op.Name {
				name = d.name
			}
		}
		for _, d := range aiTools {
			if d.operation == op.Name {
				name = d.name
			}
		}
		for _, d := range scopedBatchTools {
			if d.operation == op.Name {
				name = d.name
			}
		}
		if op.Name == "jobs" {
			name = "uns_jobs_query"
		}
		if name != "" {
			routes = append(routes, codeRoute{operation: op, tool: name})
		}
	}
	subject, _ := contextx.SubjectFrom(ctx)
	if s.fleet != nil && subject.APIKeyAppID == "" {
		for _, contract := range fleetapi.Contracts() {
			permission, scope := fleetapi.OperationAccess(contract.Name, subject)
			if !contextx.HasResource(subject, permission) || subject.MCPRestricted && !mcpaccess.Has(subject, scope) {
				continue
			}
			var schema map[string]any
			if json.Unmarshal(contract.Schema, &schema) != nil {
				continue
			}
			routes = append(routes, codeRoute{operation: operation{Name: contract.Name, Suffix: contract.Suffix, Summary: contract.Description, CoreOnly: true}, tool: "fleet_" + contract.Name, requestSchema: schema})
		}
	}
	return routes
}

func (s *Service) codeSpec(ctx context.Context) string {
	paths := map[string]any{}
	contract := catalog()
	for _, route := range s.codeRoutes(ctx) {
		op := route.operation
		requestSchema := inlineCodeSchema(contract.Schemas[op.Request], contract.Schemas, map[string]bool{}, 0)
		if route.requestSchema != nil {
			requestSchema = route.requestSchema
		}
		params := []any{}
		for _, name := range op.PathParams {
			params = append(params, map[string]any{"name": name, "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
		}
		paths[codePath(op)] = map[string]any{"post": map[string]any{
			"operationId": op.Name, "summary": op.Summary, "parameters": params,
			"requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": requestSchema}}},
			"responses":   map[string]any{"200": map[string]any{"description": "Operation response", "content": map[string]any{"application/json": map[string]any{"schema": inlineCodeSchema(contract.Schemas[op.Response], contract.Schemas, map[string]bool{}, 0)}}}},
		}}
	}
	raw, _ := json.Marshal(map[string]any{"paths": paths})
	return string(raw)
}

func inlineCodeSchema(value any, schemas map[string]any, seen map[string]bool, depth int) any {
	if depth > 32 {
		return map[string]any{}
	}
	switch node := value.(type) {
	case map[string]any:
		if ref, ok := node["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			if seen[name] {
				return map[string]any{}
			}
			seen[name] = true
			result := inlineCodeSchema(schemas[name], schemas, seen, depth+1)
			delete(seen, name)
			return result
		}
		out := map[string]any{}
		for key, child := range node {
			out[key] = inlineCodeSchema(child, schemas, seen, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, child := range node {
			out[i] = inlineCodeSchema(child, schemas, seen, depth+1)
		}
		return out
	default:
		return value
	}
}

func (s *Service) addCodeTools(server *mcp.Server, subject contextx.Subject) {
	mcp.AddTool(server, readOnlyTool("whoami", "Return the authenticated identity, effective permissions and auth method. Enterprise has one local workspace, identified as 1; no cloud workspace is implied."), func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		permissions := []string{}
		for key := range subject.ResourceKeys {
			permissions = append(permissions, key)
		}
		sort.Strings(permissions)
		method := "api_key"
		if subject.AuthType == "mcpOAuth" {
			method = "oauth"
		}
		kind := "user"
		if subject.KeyType == "service" {
			kind = "service"
		}
		out := map[string]any{"id": "user:" + strconv.FormatInt(subject.UserID, 10), "kind": kind, "userId": strconv.FormatInt(subject.UserID, 10), "name": subject.UserName, "workspaceId": strconv.FormatInt(common.PlatformWorkspaceID, 10), "permissions": permissions, "keyType": subject.KeyType, "keyName": subject.APIKeyName, "authMethod": method}
		if subject.APIKeyAppID != "" {
			out["id"] = "app:" + subject.APIKeyAppID
			out["kind"] = "app"
			out["appId"] = subject.APIKeyAppID
		}
		return finish(out, nil)
	})
	mcp.AddTool(server, readOnlyTool("docs", "Search bundled documentation with query, or read a complete source document using offset/pageSize (Unicode characters, default 12000, maximum 24000). Continue with nextOffset and the same sha256 until truncated=false. Start Source onboarding at sources/README.md. Documentation does not grant execution rights."), func(ctx context.Context, _ *mcp.CallToolRequest, in agentDocsInput) (*mcp.CallToolResult, any, error) {
		value, err := s.agentDocs(contextx.WithSubject(ctx, subject), in)
		return finish(value, err)
	})
	mcp.AddTool(server, readOnlyTool("search", "Run a JavaScript async arrow function over read-only spec.paths. Paths are full /openapi/v2 paths; operations include inline requestBody and response schemas. Example: async () => Object.keys(spec.paths). No network, filesystem or timer APIs."), func(ctx context.Context, _ *mcp.CallToolRequest, in codeInput) (*mcp.CallToolResult, any, error) {
		ctx = contextx.WithSubject(ctx, subject)
		return s.codeResult(ctx, sandboxJob{Mode: sandboxModeSearch, Code: in.Code, Spec: s.codeSpec(ctx)}, nil)
	})
	tool := operationTool("execute", "Run a JavaScript async arrow function. tier0.request({method,path,fleetNodeId,query,body}) returns {status,code,msg,data,requestId}; use POST and exact paths from search. New Source document and credentials go in one atomic documents/create request. For supplied credentialMaterial, use credentialImport:'session' on one Source create or credentials/update call; material stays outside JavaScript. Every call retains normal permissions and target checks. The tool returns contractVersion, result, outcome, error, calls and notices as one JSON envelope. Reconcile outcome=unknown before retrying; never print secrets. workspaceId is 1. No arbitrary network/filesystem access.", true)
	tool.Annotations.IdempotentHint = false
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in executeInput) (*mcp.CallToolResult, any, error) {
		ctx = contextx.WithSubject(ctx, subject)
		if in.WorkspaceID != "" && in.WorkspaceID != strconv.FormatInt(common.PlatformWorkspaceID, 10) {
			return finish(nil, errors.New("workspaceId does not match this Enterprise installation"))
		}
		var inner *mcp.ServerSession
		var client *mcp.ClientSession
		defer func() {
			if client != nil {
				_ = client.Close()
			}
			if inner != nil {
				_ = inner.Close()
			}
		}()
		uncertain := false
		calls := []map[string]any{}
		host := func(callCtx context.Context, req apiRequest) (response *apiResponse, requestErr error) {
			mutating, dispatched := true, false
			defer func() {
				call := map[string]any{"method": req.Method, "path": req.Path, "status": 0, "outcome": "failed", "retried": false, "error": nil}
				if req.FleetNodeID != "" {
					call["fleetNodeId"] = req.FleetNodeID
				}
				if response != nil {
					call["status"] = response.Status
					if response.Status >= 200 && response.Status < 300 {
						call["outcome"] = "ok"
					} else {
						var body struct {
							Error map[string]any `json:"error"`
						}
						_ = json.Unmarshal([]byte(response.Body), &body)
						call["error"] = body.Error
						code, _ := body.Error["code"].(string)
						execution, _ := body.Error["execution"].(string)
						rolled, _ := body.Error["rolledBack"].(bool)
						if mutating && dispatched && !rolled && execution != "notStarted" && execution != "rolledBack" && code != "FORBIDDEN" && code != "INVALID_REQUEST" && code != "VERSION_CONFLICT" {
							call["outcome"] = "unknown"
							uncertain = true
						}
					}
				}
				if requestErr != nil {
					call["error"] = map[string]any{"code": "MCP_REQUEST_FAILED", "message": requestErr.Error()}
					if mutating && dispatched {
						call["outcome"] = "unknown"
						uncertain = true
					}
				}
				calls = append(calls, call)
			}()
			if req.CredentialImport != "" {
				if req.CredentialImport != "session" || len(in.CredentialMaterial) == 0 || req.Method != "POST" || (req.Path != "/openapi/v2/uns/sources/documents/create" && req.Path != "/openapi/v2/uns/sources/documents/credentials/update") || req.RawBody != nil || len(req.Query) > 0 || req.ContentType != "" {
					return nil, errors.New("credentialImport requires one authorized Source create or rotation request")
				}
				var body map[string]json.RawMessage
				if json.Unmarshal(req.Body, &body) != nil || body == nil || body["credentials"] != nil || body["items"] != nil {
					return nil, errors.New("credentialImport requires a single Source without inline credentials")
				}
				material, err := json.Marshal(in.CredentialMaterial)
				if err != nil {
					return nil, errors.New("invalid credential material")
				}
				body["credentials"] = material
				req.Body, err = json.Marshal(body)
				if err != nil {
					return nil, errors.New("invalid credential request")
				}
				clear(in.CredentialMaterial)
				in.CredentialMaterial = nil
			}
			route, args, err := s.resolveCodeRequest(ctx, req)
			if err != nil {
				return nil, err
			}
			if req.FleetNodeID != "" && route.operation.CoreOnly {
				return nil, errors.New("Fleet management calls run on the current node; fleetNodeId selects only target-native operations")
			}
			// Create the bounded local dispatcher only after the runner admitted
			// this call. Queued/invalid scripts allocate no additional MCP server.
			if client == nil && req.FleetNodeID == "" {
				ct, st := mcp.NewInMemoryTransports()
				request, _ := http.NewRequestWithContext(callCtx, http.MethodPost, "http://localhost/mcp?codemode=false", nil)
				inner, err = s.buildServer(request).Connect(callCtx, st, nil)
				if err != nil {
					return nil, err
				}
				client, err = mcp.NewClient(&mcp.Implementation{Name: "enterprise-codemode", Version: s.version}, nil).Connect(callCtx, ct, nil)
				if err != nil {
					return nil, err
				}
			}
			// Outcome remains unknown until the actual handler confirms a result.
			mutating = true
			if op, ok := unsv2.FindResourceOperation(route.operation.Name); ok {
				mutating = op.Mutation != ""
			} else {
				for _, d := range append(queryTools, aiTools...) {
					if d.operation == route.operation.Name {
						mutating = false
					}
				}
			}
			var callResult *mcp.CallToolResult
			if req.FleetNodeID != "" {
				if s.fleet == nil {
					return nil, errors.New("Fleet runtime unavailable")
				}
				body, encodeErr := json.Marshal(args)
				if encodeErr != nil {
					return nil, encodeErr
				}
				dispatched = true
				value, callErr := s.fleet.ExecuteNative(contextx.WithSubject(callCtx, subject), req.FleetNodeID, route.operation.Name, body)
				callResult, err = result(value, safeMutationError(callErr))
			} else {
				dispatched = true
				callResult, err = client.CallTool(callCtx, &mcp.CallToolParams{Name: route.tool, Arguments: args})
			}
			if err != nil {
				uncertain = uncertain || mutating
				return nil, err
			}
			raw := "null"
			for _, content := range callResult.Content {
				if text, ok := content.(*mcp.TextContent); ok {
					raw = text.Text
					break
				}
			}
			if callResult.IsError {
				var failure struct {
					Error struct {
						RolledBack bool   `json:"rolledBack"`
						Code       string `json:"code"`
					} `json:"error"`
				}
				_ = json.Unmarshal([]byte(raw), &failure)
				definite := failure.Error.RolledBack || failure.Error.Code == "FORBIDDEN" || failure.Error.Code == "INVALID_REQUEST" || failure.Error.Code == "VERSION_CONFLICT"
				uncertain = uncertain || mutating && !definite
				status := 400
				switch failure.Error.Code {
				case "FORBIDDEN":
					status = 403
				case "NOT_FOUND":
					status = 404
				case "VERSION_CONFLICT", "CONFLICT", "DUPLICATE":
					status = 409
				case "QUERY_FAILED", "INTERNAL_ERROR":
					status = 500
				}
				var fault map[string]any
				_ = json.Unmarshal([]byte(raw), &fault)
				envelope, _ := json.Marshal(map[string]any{"code": status, "msg": "Tier0 operation failed", "error": fault["error"], "data": nil})
				return &apiResponse{Status: status, ContentType: "application/json", Body: string(envelope)}, nil
			}
			return &apiResponse{Status: 200, ContentType: "application/json", Body: raw}, nil
		}
		if strings.TrimSpace(in.Code) == "" || len(in.Code) > 65536 {
			return nativeExecutionResult("", errors.New("code must contain 1..65536 bytes"), false, calls), nil, nil
		}
		raw, err := s.runner.run(ctx, sandboxJob{Mode: sandboxModeExecute, Code: in.Code, WorkspaceID: strconv.FormatInt(common.PlatformWorkspaceID, 10)}, host)
		return nativeExecutionResult(raw, err, uncertain, calls), nil, nil
	})
}

func (s *Service) codeResult(ctx context.Context, job sandboxJob, host sandboxHost) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(job.Code) == "" || len(job.Code) > 65536 {
		return finish(nil, errors.New("code must contain 1..65536 bytes"))
	}
	raw, err := s.runner.run(ctx, job, host)
	if err != nil {
		var codeErr *codeError
		if errors.As(err, &codeErr) {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: codeErr.Error()}}}, nil, nil
		}
		return finish(nil, err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: raw}}}, nil, nil
}

func (s *Service) resolveCodeRequest(ctx context.Context, req apiRequest) (codeRoute, map[string]any, error) {
	fail := func() (codeRoute, map[string]any, error) {
		return codeRoute{}, nil, errors.New("request must match an authorized POST operation from search; raw bodies, queries and arbitrary URLs are not supported by local UNS operations")
	}
	if req.Method != "POST" || req.RawBody != nil || len(req.Query) > 0 || req.ContentType != "" && req.ContentType != "application/json" || strings.ContainsAny(req.Path, "?#\\") {
		return fail()
	}
	for _, route := range s.codeRoutes(ctx) {
		template := strings.Split(codePath(route.operation), "/")
		actual := strings.Split(req.Path, "/")
		if len(template) != len(actual) {
			continue
		}
		params := map[string]string{}
		matched := true
		for i, part := range template {
			if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
				value, err := url.PathUnescape(actual[i])
				if err != nil || value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\\x00") {
					matched = false
					break
				}
				params[strings.Trim(part, "{}")] = value
			} else if part != actual[i] {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		args := map[string]any{}
		if len(req.Body) > 0 {
			decoder := json.NewDecoder(strings.NewReader(string(req.Body)))
			decoder.UseNumber()
			if err := decoder.Decode(&args); err != nil || args == nil {
				return fail()
			}
		}
		for key, value := range params {
			if old, ok := args[key]; ok && old != value {
				return fail()
			}
			args[key] = value
		}
		return route, args, nil
	}
	return fail()
}
