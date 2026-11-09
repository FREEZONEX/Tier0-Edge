package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"backend/internal/contextx"
	"backend/internal/domain/fleetapi"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsv2"
	"backend/internal/mcpaccess"
	"backend/internal/repo"
)

type toolDefinition struct {
	name        string
	description string
	operation   string
	kinds       []string
	permissions []string
}

func (d toolDefinition) MarshalJSON() ([]byte, error) {
	readOnly := !strings.HasPrefix(d.name, "fleet_") && d.name != "execute"
	for _, scoped := range scopedBatchTools {
		if scoped.name == d.name {
			readOnly = strings.HasSuffix(scoped.operation, "Validate") || strings.HasSuffix(scoped.operation, "Status")
		}
	}
	if operation, ok := unsv2.FindResourceOperation(d.operation); ok {
		readOnly = operation.Mutation == ""
	}
	return json.Marshal(map[string]any{"name": d.name, "description": d.description, "permissions": d.permissions, "readOnly": readOnly})
}

var queryTools = []toolDefinition{
	{"uns_namespace_query", "Read directory/Topic children and optional ancestors. Use parentId explicitly; a Topic is a leaf, not a Collection.", "namespace", nil, []string{"uns.read"}},
	{"uns_topic_view", "Resolve explicit Topic field bindings to current Record fields or latest Sample fields. This describes one object and never performs an inferred cross-collection join.", "topics", nil, []string{"uns.read"}},
	{"uns_references_query", "Read explicit Topic, Source instance and App grant references to a Collection, Topic or Stream. Hidden references are flagged, not disclosed.", "references", nil, []string{"uns.read"}},
}

func (s *Service) allowedTools(ctx context.Context) []toolDefinition {
	allowed := []toolDefinition{}
	if !permitted(ctx, "uns.read") {
		return allowed
	}
	names := []string{"docs", "search", "execute", "whoami"}
	subject, _ := contextx.SubjectFrom(ctx)
	if subject.APIKeyUsageType != repo.APIKeyUsageAppData {
		names = append(names, "instance_capabilities", "skills_list", "skills_read", "api_docs_search", "api_contract_get", "api_examples_get", "api_operations_list")
	}
	for _, name := range names {
		allowed = append(allowed, toolDefinition{name: name, permissions: []string{"uns.read"}})
	}
	for _, d := range queryTools {
		if operationExecutable(ctx, d.operation) && permitted(ctx, d.permissions...) {
			allowed = append(allowed, d)
		}
	}
	for _, d := range aiTools {
		if _, exists := findOperation(catalog(), d.operation); exists && aiToolAllowed(ctx, d) {
			d.operation = "" // Registered with its flat contract by addAITools.
			allowed = append(allowed, d)
		}
	}
	if subject, ok := contextx.SubjectFrom(ctx); ok && subject.APIKeyUsageType != repo.APIKeyUsageAppData && mcpaccess.CanWrite(subject) {
		if _, exists := findOperation(catalog(), "jobs"); exists {
			allowed = append(allowed, toolDefinition{name: "uns_jobs_query", description: "Read owned authorized batch jobs", permissions: subject.MCPScopes})
		}
	}
	for _, operation := range unsv2.ResourceOperations() {
		if resourceExecutable(ctx, operation) {
			allowed = append(allowed, toolDefinition{name: resourceToolName(operation.Name), description: operation.Summary, operation: operation.Name, permissions: []string{operation.Permission}})
		}
	}
	if subject.APIKeyUsageType == repo.APIKeyUsageAppData {
		return allowed
	}
	for _, d := range scopedBatchTools {
		if scopedBatchAllowed(ctx, d.operation) {
			d.operation = "" // Registered with the shared flat contract separately.
			allowed = append(allowed, d)
		}
	}
	if s.fleet != nil {
		subject, _ := contextx.SubjectFrom(ctx)
		for _, contract := range fleetapi.Contracts() {
			permission, scope := fleetapi.OperationAccess(contract.Name, subject)
			if contextx.HasResource(subject, permission) && (!subject.MCPRestricted || mcpaccess.Has(subject, scope)) {
				allowed = append(allowed, toolDefinition{name: "fleet_" + contract.Name, description: contract.Description, permissions: []string{permission}})
			}
		}
	}
	return allowed
}

// Contract discovery does not make newly registered APIs callable over MCP.
func operationExecutable(ctx context.Context, name string) bool {
	if subject, _ := contextx.SubjectFrom(ctx); subject.APIKeyUsageType == repo.APIKeyUsageAppData && !unsv2.IsAppOperationAllowed(name) {
		return false
	}
	if _, exists := findOperation(catalog(), name); !exists {
		return false
	}
	if retiredDataQueryOperation(name) {
		return false
	}
	if unsv2.BatchScope(name) != "" {
		return scopedBatchAllowed(ctx, name)
	}
	for _, d := range aiTools {
		if d.operation == name {
			return aiToolAllowed(ctx, d)
		}
	}
	if operation, ok := unsv2.FindResourceOperation(name); ok {
		return resourceExecutable(ctx, operation)
	}
	if name == "jobs" {
		subject, _ := contextx.SubjectFrom(ctx)
		return mcpaccess.CanWrite(subject)
	}
	if name == "catalog" {
		return permitted(ctx, "uns.read") // Exposed through instance_capabilities.
	}
	for _, tool := range queryTools {
		if tool.operation == name && permitted(ctx, tool.permissions...) {
			return true
		}
	}
	return false
}

func operationDocumentationAllowed(ctx context.Context, name string) bool {
	if retiredDataQueryOperation(name) {
		return false
	}
	if operation, ok := unsv2.FindResourceOperation(name); ok {
		return permitted(ctx, operation.Permission) || permitted(ctx, modelPermission(operation.Resource))
	}
	switch name {
	case "appContext", "appKeyCreate", "appKeyRevoke":
		return permitted(ctx, "uns.app.read") || permitted(ctx, "uns.app.manage")
	}
	return true
}

func retiredDataQueryOperation(name string) bool {
	switch name {
	case "records", "samples", "collections", "recordGet", "recordList", "recordSearch", "recordAggregate", "recordQueryValidate", "sampleLatest", "sampleHistory", "sampleTrend", "recordCreate", "recordUpdate", "recordReplace", "recordDelete", "sampleQuerySeries", "recordTransactionValidate", "apply", "transactionExecute", "transactionValidate", "transactionStatus", "recordBatchExecute", "recordBatchValidate", "recordBatchStatus", "sampleAppendBatch", "sampleAppendBatchStatus", "metaBatchStatus":
		return true
	}
	return false
}

func (s *Service) query(ctx context.Context, d toolDefinition, input unsv2.Queries) (any, error) {
	if !permitted(ctx, d.permissions...) {
		return nil, &unsv2.Fault{Code: "FORBIDDEN", Message: "Required read permission is missing"}
	}
	for i := range input.Queries {
		q := &input.Queries[i]
		if err := relationalReadPolicy(q.Relational); err != nil {
			return nil, err
		}
		if q.Size > 200 || q.Records != nil && q.Records.Size > 200 || q.SelectedRecord != nil && q.SelectedRecord.History != nil && q.SelectedRecord.History.Size > 200 {
			return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Message: "MCP query page sizes must not exceed 200"}
		}
		if d.kinds != nil {
			q.Kinds = append([]string(nil), d.kinds...)
		}
	}
	value, err := s.uns.Query(ctx, d.operation, input)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	// Relational columns have arbitrary caller-selected aliases, including kind,
	// data and binding. They must not be interpreted as metadata envelopes.
	// Their scalar source fields and output aliases were checked before execution.
	root := object(decoded)
	for i, entry := range array(root["results"]) {
		if i < len(input.Queries) && input.Queries[i].Relational != nil {
			continue
		}
		array(root["results"])[i] = sanitize(entry)
	}
	return root, nil
}

// Validate source paths before SQL executes: post-query key redaction cannot
// recover provenance after aliasing, grouping, aggregation or filtering.
func relationalReadPolicy(plan *unsv2.RelationalQuery) error {
	if plan == nil {
		return nil
	}
	check := func(path string) error {
		for _, segment := range strings.Split(path, ".") {
			if sensitive(segment) {
				return &unsv2.Fault{Code: "FORBIDDEN", Path: "relational", Message: "Protected credential fields cannot participate in MCP relational queries"}
			}
		}
		return nil
	}
	expressionNodes := 0
	var checkExpression func(*unsv2.QueryExpression, int) error
	checkExpression = func(expression *unsv2.QueryExpression, depth int) error {
		if expression == nil {
			return nil
		}
		expressionNodes++
		if depth > 6 || expressionNodes > 64 {
			return &unsv2.Fault{Code: "INVALID_REQUEST", Path: "relational.select.expression", Message: "Expression limit exceeded"}
		}
		if err := check(expression.Field); err != nil {
			return err
		}
		for i := range expression.Args {
			if err := checkExpression(&expression.Args[i], depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	// Check the complete provenance before derived columns can hide their source
	// fields behind innocuous aliases. Subqueries cannot themselves be nested.
	for _, subquery := range plan.Subqueries {
		if err := relationalReadPolicy(&unsv2.RelationalQuery{
			Joins: subquery.Joins, Select: subquery.Select, Where: subquery.Where,
			GroupBy: subquery.GroupBy, Having: subquery.Having,
		}); err != nil {
			return err
		}
	}
	for _, selection := range plan.Select {
		if err := checkExpression(selection.Expression, 1); err != nil {
			return err
		}
		if err := check(selection.Field); err != nil {
			return err
		}
		if err := check(selection.As); err != nil {
			return err
		}
	}
	for _, join := range plan.Joins {
		for _, on := range join.On {
			if err := check(on.Left); err != nil {
				return err
			}
			if err := check(on.Right); err != nil {
				return err
			}
		}
	}
	for _, field := range plan.GroupBy {
		if err := check(field); err != nil {
			return err
		}
	}
	for _, filter := range plan.Having {
		if err := check(filter.Field); err != nil {
			return err
		}
	}
	for _, order := range plan.OrderBy {
		if err := check(order.Field); err != nil {
			return err
		}
	}
	nodes := 0
	var walk func(*unsv2.QueryPredicate, int) error
	walk = func(p *unsv2.QueryPredicate, depth int) error {
		if p == nil {
			return nil
		}
		nodes++
		if depth > 6 || nodes > 64 {
			return &unsv2.Fault{Code: "INVALID_REQUEST", Path: "relational.where", Message: "Predicate limit exceeded"}
		}
		if err := check(p.Field); err != nil {
			return err
		}
		for i := range p.And {
			if err := walk(&p.And[i], depth+1); err != nil {
				return err
			}
		}
		for i := range p.Or {
			if err := walk(&p.Or[i], depth+1); err != nil {
				return err
			}
		}
		return walk(p.Not, depth+1)
	}
	return walk(plan.Where, 1)
}

func sensitive(key string) bool {
	return mcpaccess.IsCredentialField(key)
}

// 脱敏只用于凭据值；关系仍由显式 ID/路径决定，不由名称推断。
func redactData(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			if sensitive(key) {
				out[key] = "[REDACTED]"
			} else {
				out[key] = redactData(item)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = redactData(item)
		}
		return out
	default:
		return value
	}
}

func pick(v map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, key := range keys {
		if value, ok := v[key]; ok {
			out[key] = value
		}
	}
	return out
}
func object(value any) map[string]any {
	v, _ := value.(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}

func sanitize(value any) any {
	switch v := value.(type) {
	case []any:
		for i, item := range v {
			v[i] = sanitize(item)
		}
		return v
	case map[string]any:
		// Returned Record snapshots are arbitrary business documents, just like
		// pipeline results. Do not interpret their nested kind/data fields as
		// platform metadata envelopes.
		if document, ok := v["document"]; ok {
			v["document"] = redactData(document)
		}
		if kind, _ := v["kind"].(string); kind != "" && v["data"] != nil {
			data := object(v["data"])
			switch kind {
			case "source":
				v["data"] = sourceProjection(data)
			case "source_state":
				v["data"] = stateProjection(data)
			case "stream":
				v["data"] = redactMetadataSchemas(streamProjection(data))
			case "app":
				v["data"] = pick(data, "schemaVersion", "name", "description", "status", "collections", "navigation")
			case "collection":
				v["data"] = redactMetadataSchemas(data)
				return v
			case "directory", "topic":
				return v
			}
			return v
		}
		for key, item := range v {
			if key == "recordSchema" || key == "sampleSchema" || key == "parametersSchema" {
				v[key] = redactFieldSchema(item)
			} else if key == "fields" || key == "relationships" || key == "semantics" {
				v[key] = sanitize(redactMetadataSchemas(map[string]any{key: item}).(map[string]any)[key])
			} else if key == "data" {
				v[key] = redactData(item)
			} else {
				v[key] = sanitize(item)
			}
		}
		if binding := object(v["binding"]); len(binding) > 0 {
			v["value"] = redactData(v["value"])
			parts, pathErr := unsmeta.ParseFieldPath(stringValue(binding["fieldPath"]))
			if pathErr != nil {
				v["value"], v["currentValue"] = "[REDACTED]", "[REDACTED]"
			}
			for _, part := range parts {
				if sensitive(part) {
					v["value"] = "[REDACTED]"
					v["currentValue"] = "[REDACTED]"
					break
				}
			}
		}
		return v
	default:
		return value
	}
}

func stringValue(value any) string { s, _ := value.(string); return s }
