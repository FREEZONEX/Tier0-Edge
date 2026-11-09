package mcpserver

import (
	"backend/internal/contextx"
	"backend/internal/domain/unsv2"
	"backend/internal/mcpaccess"
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
)

type DiscoveryInput struct {
	Resource string   `json:"resource,omitempty"`
	Kinds    []string `json:"kinds,omitempty"`
	Search   string   `json:"search,omitempty"`
}

func modelAllowed(ctx context.Context, m unsv2.ModelInfo) bool {
	return permitted(ctx, m.ReadPermission) || m.WritePermission != "" && permitted(ctx, m.WritePermission)
}
func modelExecutable(ctx context.Context, m unsv2.ModelInfo, action string) bool {
	if operation, ok := unsv2.FindResourceOperation(mutationOperation(m.Kind, action)); ok {
		return resourceExecutable(ctx, operation)
	}
	subject, _ := contextx.SubjectFrom(ctx)
	kind := m.Kind
	if kind == "record" || kind == "sample" || kind == "release" {
		kind = ""
	}
	return operationExecutable(ctx, mutationOperation(m.Kind, action)) && mcpaccess.AllowedMutation(subject, action, kind, false) && mcpaccess.CanWrite(subject)
}
func (s *Service) operations(ctx context.Context, input DiscoveryInput) (any, error) {
	if input.Resource != "" {
		input.Kinds = append(input.Kinds, input.Resource)
	}
	if len(input.Kinds) > 20 || len(input.Search) > 128 {
		return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Path: "kinds/search", Reason: "Use at most 20 kind filters and 128 search characters", Repair: "Call with an empty object to discover names first"}
	}
	models := []map[string]any{}
	wanted := map[string]bool{}
	ops := map[string]bool{}
	for _, kind := range input.Kinds {
		if _, ok := unsv2.Model(kind); !ok {
			return nil, s.kindFault(ctx, kind)
		}
		wanted[kind] = true
	}
	for _, model := range unsv2.Models() {
		if len(wanted) > 0 && !wanted[model.Kind] {
			continue
		}
		if input.Search != "" && !strings.Contains(strings.ToLower(model.Kind+" "+model.Boundary), strings.ToLower(input.Search)) {
			continue
		}
		entry := map[string]any{"kind": model.Kind, "storage": model.Storage, "schema": model.Schema, "definitionAvailable": modelAllowed(ctx, model), "readPermission": model.ReadPermission, "writePermission": model.WritePermission, "boundary": model.Boundary}
		actions := []map[string]any{}
		if modelAllowed(ctx, model) {
			for _, name := range model.Operations {
				ops[name] = true
			}
			for _, operation := range unsv2.ResourceOperations() {
				if operation.Resource == model.Kind {
					ops[operation.Name] = true
				}
			}
			for _, action := range model.Actions {
				includeMutationOperations(ops, model.Kind, action)
				actions = append(actions, map[string]any{"action": action, "operation": mutationOperation(model.Kind, action), "mcpExecutable": modelExecutable(ctx, model, action), "mcpTool": mutationTool(model.Kind, action)})
			}
		}
		entry["actions"] = actions
		models = append(models, entry)
	}
	operations := []map[string]any{}
	for _, o := range catalog().Operations {
		if len(wanted) > 0 && !ops[o.Name] {
			continue
		}
		if !operationDocumentationAllowed(ctx, o.Name) {
			continue
		}
		entry := operationView(ctx, o)
		operations = append(operations, entry)
	}
	return map[string]any{"productVersion": s.version, "contractVersion": unsv2.Catalog()["contractVersion"], "models": models, "operations": operations, "naming": map[string]any{"operation": "HTTP interface name or operationId, e.g. recordTransactionExecute / UnsV2RecordTransactionExecute", "action": "Metadata operations[].action uses insertOne/updateOne; Record items[].action uses record.create; these are not HTTP endpoint names", "kind": "Metadata discriminator, e.g. source; Record/Sample actions omit kind"}, "next": "Read schemas with api_contract_get. Use collection-scoped Record and Sample resource tools; use uns_records_transaction for business composition and uns_metadata_transact or uns_metadata_bulk_write for metadata. Metadata writes use dryRun=true for rollback previews where declared, defaulting to false; Record transactions still require explicit dryRun=true/false. Record filter/pipeline fields are bare business names or _meta system fields; ordinary results put business values in data. Sample find/aggregate accept multiple recordIds plus required from/to; latest/delete require explicit recordIds. Optional idempotencyKey and revision guards apply where declared. Accepted jobId is queried with uns_jobs_query. Deletes require separate consent. Cross-scope atomicity is not provided."}, nil
}
func mutationOperation(kind, action string) string {
	if (kind == "directory" || kind == "topic") && action == "meta.copy" {
		return "namespaceCopy"
	}
	resource := kind
	if kind == "release" {
		resource = "stream"
	}
	if kind == "directory" || kind == "topic" || kind == "collection" {
		resource = "metadata"
	}
	for _, operation := range unsv2.ResourceOperations() {
		if operation.Resource == resource && (operation.Name == action || operation.MetadataAction == action || operation.NovaAction == action || operation.Mutation == action) {
			return operation.Name
		}
	}
	if strings.HasPrefix(action, "record.") {
		return "recordTransactionExecute"
	}
	if action == "sample.append" {
		return "sampleAppend"
	}
	return ""
}
func mutationTool(kind, action string) string {
	if operation, ok := unsv2.FindResourceOperation(mutationOperation(kind, action)); ok {
		return resourceToolName(operation.Name)
	}
	for _, d := range scopedBatchTools {
		if d.operation == mutationOperation(kind, action) {
			return d.name
		}
	}
	return ""
}

// Discovery by model must include preview and recovery as well as submission.
func includeMutationOperations(names map[string]bool, kind, action string) {
	name := mutationOperation(kind, action)
	if name == "" {
		return
	}
	scope := unsv2.BatchScope(name)
	if scope == "" {
		names[name] = true
		return
	}
	for _, definition := range scopedBatchTools {
		if unsv2.BatchScope(definition.operation) == scope {
			names[definition.operation] = true
		}
	}
}

func operationView(ctx context.Context, o operation) map[string]any {
	if resource, ok := unsv2.FindResourceOperation(o.Name); ok {
		entry := map[string]any{"name": o.Name, "operation": o.Name, "operationId": o.Handler, "resource": resource.Resource, "method": "POST", "suffix": o.Suffix, "path": "/openapi/v2" + o.Suffix, "publicPath": "/openapi/v2" + o.Suffix, "internalPath": "/api/core/v2" + o.Suffix, "summary": resource.Summary, "permission": resource.Permission, "permissions": resource.Permission, "mcpExecutable": resourceExecutable(ctx, resource), "mcpTools": []string{}, "status": "implemented"}
		if resourceExecutable(ctx, resource) {
			entry["tool"] = resourceToolName(o.Name)
			entry["mcpTools"] = []string{resourceToolName(o.Name)}
		}
		return entry
	}
	internal := "/api/core/v2" + o.Suffix
	public := "/openapi/v2" + o.Suffix
	if o.CoreOnly {
		public = ""
	}
	if o.OpenAPIOnly {
		internal = ""
	}
	tools := []string{}
	for _, d := range scopedBatchTools {
		if d.operation == o.Name && scopedBatchAllowed(ctx, o.Name) {
			tools = append(tools, d.name)
		}
	}
	for _, d := range aiTools {
		if d.operation == o.Name && aiToolAllowed(ctx, d) && !strings.HasPrefix(d.description, "Deprecated") {
			tools = append(tools, d.name)
		}
	}
	for _, t := range queryTools {
		if t.operation == o.Name && permitted(ctx, t.permissions...) {
			tools = append(tools, t.name)
		}
	}
	subject, _ := contextx.SubjectFrom(ctx)
	if o.Name == "jobs" && mcpaccess.CanWrite(subject) {
		tools = append(tools, "uns_jobs_query")
	}
	permission := "uns.read"
	if o.Name == "appKeyCreate" || o.Name == "appKeyRevoke" {
		permission = "uns.app.manage + uns.app.credentials"
	}
	if unsv2.BatchScope(o.Name) != "" {
		permission = "Per-item model/data/configuration write permission; delete additionally requires uns.delete"
	}
	entry := map[string]any{"operation": o.Name, "operationId": o.Handler, "method": "POST", "suffix": o.Suffix, "publicPath": public, "internalPath": internal, "permissions": permission, "mcpExecutable": operationExecutable(ctx, o.Name), "mcpTools": tools, "status": "implemented", "boundary": map[bool]string{true: "Uses current permissions and MCP consent", false: "Documentation only; no MCP execution tool"}[operationExecutable(ctx, o.Name)]}
	if o.CoreOnly {
		entry["boundary"] = "Internal Session-authenticated interface only; no OpenAPI or MCP execution endpoint"
		entry["mcpExecutable"] = false
	}
	return entry
}
func (s *Service) kindFault(ctx context.Context, kind string) error {
	candidates := []map[string]any{}
	for _, m := range unsv2.Models() {
		candidates = append(candidates, map[string]any{"kind": m.Kind, "available": modelAllowed(ctx, m)})
	}
	return &unsv2.Fault{Code: "UNKNOWN_KIND", Path: "kinds", Reason: "The requested kind is not in the current model catalog", Repair: "Select an exact kind from candidates; no operation was executed", Candidates: candidates}
}
func (s *Service) operationFault(ctx context.Context, name string) error {
	candidates := []map[string]any{}
	for _, o := range catalog().Operations {
		if operationDocumentationAllowed(ctx, o.Name) {
			candidates = append(candidates, operationView(ctx, o))
		}
	}
	reason := "Unknown interface operation"
	if strings.Contains(name, ".") {
		reason = "Batch action is not an interface operation; use metadataTransact with operations[].action or recordTransactionExecute with items[].action, or choose a resource operation from the catalog"
	}
	return &unsv2.Fault{Code: "UNKNOWN_OPERATION", Path: "operations", Reason: reason, Repair: "Call api_operations_list or choose an exact operation/operationId/path from candidates", Candidates: candidates}
}

func (s *Service) addDiscoveryTools(server *mcp.Server, subject contextx.Subject) {
	mcp.AddTool(server, readOnlyTool("api_operations_list", "Discover all current UNS kinds, interface operations versus batch actions, HTTP paths, permissions and executable boundaries. Empty input lists the catalog. Never executes guessed names."), func(ctx context.Context, _ *mcp.CallToolRequest, input DiscoveryInput) (*mcp.CallToolResult, any, error) {
		value, err := s.operations(contextx.WithSubject(ctx, subject), input)
		return finish(value, err)
	})
}
